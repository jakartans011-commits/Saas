package compliance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	bolt "go.etcd.io/bbolt"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/auth"
	"trailseal/project"
	"trailseal/tenant"
)

// ============================================================
// SweepExpiredTenants: hanya tenant soft-deleted yang lewat grace
// period (30 hari Free/Pro, 60 hari Business) yang dipurge.
//
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go test ./compliance -run SweepExpiredTenants -v
// ============================================================

func TestSweepExpiredTenants_PurgesOnlyPastGracePeriod(t *testing.T) {
	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TRAILSEAL_TEST_POSTGRES_URL is not set; sweep integration test skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}
	t.Cleanup(pool.Close)

	boltDB, err := bolt.Open(filepath.Join(t.TempDir(), "sweep.db"), 0600, nil)
	if err != nil {
		t.Fatalf("bolt.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = boltDB.Close() })

	tenantStore, err := tenant.NewBoltTenantStore(boltDB)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}
	userStore, err := auth.NewBoltUserStore(boltDB)
	if err != nil {
		t.Fatalf("NewBoltUserStore failed: %v", err)
	}
	agentStore, err := agent.NewBoltAgentStore(boltDB)
	if err != nil {
		t.Fatalf("NewBoltAgentStore failed: %v", err)
	}
	auditStore, err := audit.NewPostgresAuditStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresAuditStore failed: %v", err)
	}

	ago := func(d time.Duration) *time.Time {
		ts := time.Now().Add(-d)
		return &ts
	}
	const day = 24 * time.Hour

	cases := []struct {
		id        string
		tier      string
		deletedAt *time.Time
		wantGone  bool
	}{
		{"sweep-free-31d", "free", ago(31 * day), true},
		{"sweep-free-1d", "free", ago(1 * day), false},
		{"sweep-biz-31d", "business", ago(31 * day), false}, // grace 60 hari
		{"sweep-biz-61d", "business", ago(61 * day), true},
		{"sweep-active", "pro", nil, false},
	}
	for _, c := range cases {
		if err := tenantStore.Put(&tenant.Tenant{ID: c.id, PlanTier: c.tier, DeletedAt: c.deletedAt}); err != nil {
			t.Fatalf("Put %s failed: %v", c.id, err)
		}
	}

	purged, err := SweepExpiredTenants(
		ctx,
		tenantStore,
		userStore,
		agentStore,
		project.NewPostgresStore(pool),
		apikey.NewPostgresStore(pool),
		auditStore,
	)
	if err != nil {
		t.Fatalf("SweepExpiredTenants returned error: %v", err)
	}
	if purged != 2 {
		t.Errorf("expected 2 tenants purged, got %d", purged)
	}

	for _, c := range cases {
		_, getErr := tenantStore.GetIncludingDeleted(c.id)
		gone := getErr != nil
		if gone != c.wantGone {
			t.Errorf("tenant %s: gone=%v, want gone=%v", c.id, gone, c.wantGone)
		}
	}
}
