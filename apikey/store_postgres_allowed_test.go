package apikey

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestIsAmongOldestActive_Postgres(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)

	tenantID := fmt.Sprintf("tenant-allowed-%d", time.Now().UnixNano())
	projectID := seedProject(t, ctx, pool, tenantID, "allowed-test")
	t.Cleanup(func() { cleanupProject(t, ctx, pool, projectID) })

	// Tiga key; created_at dipaksa deterministik: k1 tertua, k3 terbaru.
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		k := &APIKey{
			ProjectID: projectID,
			TenantID:  tenantID,
			Name:      fmt.Sprintf("key-%d", i+1),
			KeyHash:   fmt.Sprintf("hash-allowed-%d-%d", time.Now().UnixNano(), i),
			KeyPrefix: "tsk_test",
			Scope:     ScopeIngest,
		}
		if err := store.Create(ctx, k); err != nil {
			t.Fatalf("create key %d: %v", i+1, err)
		}
		ids[i] = k.ID
		if _, err := pool.Exec(ctx,
			`UPDATE api_keys SET created_at = now() - make_interval(hours => $1) WHERE id = $2`,
			10-i, k.ID); err != nil {
			t.Fatalf("set created_at: %v", err)
		}
	}

	check := func(label, keyID string, limit int, want bool) {
		t.Helper()
		got, err := store.IsAmongOldestActive(ctx, tenantID, keyID, limit)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if got != want {
			t.Errorf("%s: got %v, want %v", label, got, want)
		}
	}

	// Free (limit 1): hanya yang tertua.
	check("limit1 k1", ids[0], 1, true)
	check("limit1 k2", ids[1], 1, false)
	check("limit1 k3", ids[2], 1, false)

	// limit 2: dua tertua.
	check("limit2 k1", ids[0], 2, true)
	check("limit2 k2", ids[1], 2, true)
	check("limit2 k3", ids[2], 2, false)

	// Tanpa batas (Pro/Business): semua lolos.
	check("unlimited k3", ids[2], 0, true)

	// Revoke yang tertua: k2 menjadi yang tertua aktif (limit 1).
	if err := store.Revoke(ctx, tenantID, projectID, ids[0]); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	check("after revoke limit1 k1(revoked)", ids[0], 1, false)
	check("after revoke limit1 k2", ids[1], 1, true)
	check("after revoke limit1 k3", ids[2], 1, false)

	// Tenant lain tidak terpengaruh.
	got, err := store.IsAmongOldestActive(ctx, "tenant-lain", ids[1], 1)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("key dari tenant lain tidak boleh lolos")
	}
}
