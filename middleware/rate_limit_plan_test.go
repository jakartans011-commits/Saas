package middleware

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func newPlanTestDB(t *testing.T) *bolt.DB {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "rl.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.NoSync = true
	t.Cleanup(func() { db.Close() })
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(rateLimitBucket)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLimitForTenant_MCPResolver(t *testing.T) {
	cfg := &RateLimiterConfig{Limit: DefaultLimit, Window: DefaultWindow}

	if got := limitForTenant(cfg, RateLimitPriorityMCP, "x"); got != DefaultMCPLimit {
		t.Errorf("tanpa resolver: got %d, want %d", got, DefaultMCPLimit)
	}

	cfg.MCPLimitResolver = func(id string) uint64 {
		if id == "free" {
			return 60
		}
		return 0
	}
	if got := limitForTenant(cfg, RateLimitPriorityMCP, "free"); got != 60 {
		t.Errorf("free: got %d, want 60", got)
	}
	if got := limitForTenant(cfg, RateLimitPriorityMCP, "lain"); got != DefaultMCPLimit {
		t.Errorf("resolver 0: got %d, want %d", got, DefaultMCPLimit)
	}
	if got := limitForTenant(cfg, RateLimitPriorityNormal, "free"); got != DefaultLimit {
		t.Errorf("traffic normal tidak boleh berubah: got %d", got)
	}
}

func TestRateCounter_BlocksAfterPlanLimit(t *testing.T) {
	for _, limit := range []uint64{60, 300, 1000} {
		db := newPlanTestDB(t)
		cfg := &RateLimiterConfig{Limit: DefaultLimit, Window: 3600, DB: db}
		c := getOrCreateCounter("tenant-x", RateLimitPriorityMCP, limit, cfg)

		for i := uint64(1); i <= limit; i++ {
			ok, _, _, err := c.Allow()
			if err != nil || !ok {
				t.Fatalf("limit %d: request ke-%d harus lolos (ok=%v err=%v)", limit, i, ok, err)
			}
		}
		if ok, _, _, _ := c.Allow(); ok {
			t.Errorf("limit %d: request ke-%d harus ditolak", limit, limit+1)
		}
	}
}

// Upgrade Free -> Pro memakai hitungan tersimpan yang sama (bukan jatah baru).
func TestRateCounter_UpgradeKeepsCount(t *testing.T) {
	db := newPlanTestDB(t)
	cfg := &RateLimiterConfig{Limit: DefaultLimit, Window: 3600, DB: db}

	free := getOrCreateCounter("t", RateLimitPriorityMCP, 60, cfg)
	for i := 0; i < 60; i++ {
		if ok, _, _, _ := free.Allow(); !ok {
			t.Fatalf("free: request ke-%d harus lolos", i+1)
		}
	}
	if ok, _, _, _ := free.Allow(); ok {
		t.Fatal("free: request ke-61 harus ditolak")
	}

	pro := getOrCreateCounter("t", RateLimitPriorityMCP, 300, cfg)
	for i := 0; i < 240; i++ {
		if ok, _, _, _ := pro.Allow(); !ok {
			t.Fatalf("pro: sisa jatah ke-%d harus lolos", i+1)
		}
	}
	if ok, _, _, _ := pro.Allow(); ok {
		t.Fatal("pro: request ke-301 (total) harus ditolak")
	}
}
