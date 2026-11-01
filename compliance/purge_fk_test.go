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
// PURGE LINTAS FOREIGN KEY
//
// TestSweepExpiredTenants_PurgesOnlyPastGracePeriod membuktikan tenant
// YANG MANA yang dipurge. Ia TIDAK membuktikan purge-nya berhasil, karena
// tenant-tenant di sana tidak punya satu baris pun di Postgres — tidak ada
// foreign key yang tegang, jadi tidak ada yang bisa gagal.
//
// Test ini mengisi kekosongan itu: satu tenant dengan project, chain state,
// event, idempotency key, dan API key — persis seperti pelanggan sungguhan —
// lalu memastikan purge menghapus SEMUANYA.
//
// Tabel-tabel itu saling menunjuk tanpa ON DELETE CASCADE:
//
//	idempotency_keys -> events -> projects
//	project_chain_state ------- -> projects
//	api_keys ------------------ -> projects
//
// Jadi urutan penghapusan harus dari daun ke akar. Satu saja terlewat dan
// Postgres menolak DELETE FROM projects, purge gagal di tengah jalan, dan
// data pelanggan yang meminta dihapus tetap tinggal — kewajiban GDPR yang
// tidak terpenuhi, tanpa satu pun test yang berteriak.
//
//	$env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//	go test ./compliance -run Purge_TenantWithRealData -v
// ============================================================

func TestPurge_TenantWithRealData_ClearsEveryTable(t *testing.T) {
	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TRAILSEAL_TEST_POSTGRES_URL is not set; purge FK integration test skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const tenantID = "purge-fk-tenant"

	// Sisa run sebelumnya (mis. run yang gagal di tengah) akan membuat
	// test ini gagal karena alasan yang salah. Bersihkan dulu, dan
	// bersihkan lagi setelah selesai.
	wipe(t, pool, tenantID)
	t.Cleanup(func() { wipe(t, pool, tenantID) })

	// ---- Isi Postgres seperti pelanggan sungguhan ----

	var projectID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (tenant_id, name) VALUES ($1, $2) RETURNING id`,
		tenantID, "purge-fk-project",
	).Scan(&projectID); err != nil {
		t.Fatalf("insert project failed: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO project_chain_state (project_id, last_sequence, last_hash)
		 VALUES ($1, 1, 'a1b2c3')`,
		projectID,
	); err != nil {
		t.Fatalf("insert project_chain_state failed: %v", err)
	}

	var eventID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO events (project_id, tenant_id, sequence, event_type, hash, occurred_at)
		 VALUES ($1, $2, 1, 'purge.fk.test', 'a1b2c3', now())
		 RETURNING id`,
		projectID, tenantID,
	).Scan(&eventID); err != nil {
		t.Fatalf("insert event failed: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO idempotency_keys (project_id, idempotency_key, event_id)
		 VALUES ($1, 'purge-fk-idem', $2)`,
		projectID, eventID,
	); err != nil {
		t.Fatalf("insert idempotency_key failed: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO api_keys (project_id, tenant_id, name, key_hash, key_prefix, scope)
		 VALUES ($1, $2, 'purge-fk-key', 'hash-purge-fk-test', 'tskpurge', 'admin')`,
		projectID, tenantID,
	); err != nil {
		t.Fatalf("insert api_key failed: %v", err)
	}

	// ---- Tenant soft-deleted, lewat grace period ----

	boltDB, err := bolt.Open(filepath.Join(t.TempDir(), "purge-fk.db"), 0600, nil)
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

	deletedAt := time.Now().Add(-31 * 24 * time.Hour) // Free: grace 30 hari
	if err := tenantStore.Put(&tenant.Tenant{
		ID:        tenantID,
		PlanTier:  "free",
		DeletedAt: &deletedAt,
	}); err != nil {
		t.Fatalf("Put tenant failed: %v", err)
	}

	// ---- Purge ----

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
		t.Fatalf("purge gagal untuk tenant yang punya data sungguhan: %v\n"+
			"Inilah kegagalan yang dimaksud: tenant kosong bisa dipurge, tenant nyata tidak. "+
			"Periksa urutan penghapusan di project.PurgeAllForTenant — harus dari daun ke akar, "+
			"dan setiap tabel yang menunjuk projects(id) harus ikut dihapus.", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 tenant purged, got %d", purged)
	}

	// ---- Tidak boleh ada yang tersisa ----

	byTenant := []string{"events", "projects", "api_keys"}
	for _, table := range byTenant {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, tenantID,
		).Scan(&n); err != nil {
			t.Fatalf("count %s failed: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s: masih tersisa %d baris setelah purge", table, n)
		}
	}

	byProject := []string{"idempotency_keys", "project_chain_state"}
	for _, table := range byProject {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE project_id = $1`, projectID,
		).Scan(&n); err != nil {
			t.Fatalf("count %s failed: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s: masih tersisa %d baris setelah purge", table, n)
		}
	}

	if _, err := tenantStore.GetIncludingDeleted(tenantID); err == nil {
		t.Error("tenant record masih ada setelah purge (harusnya hard-deleted)")
	}
}

// wipe menghapus sisa data test ini dari daun ke akar. Dipakai sebelum dan
// sesudah test supaya run yang gagal di tengah tidak meracuni run berikutnya.
func wipe(t *testing.T, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	ctx := context.Background()

	stmts := []string{
		`DELETE FROM idempotency_keys    WHERE project_id IN (SELECT id FROM projects WHERE tenant_id = $1)`,
		`DELETE FROM events              WHERE project_id IN (SELECT id FROM projects WHERE tenant_id = $1)`,
		`DELETE FROM project_chain_state WHERE project_id IN (SELECT id FROM projects WHERE tenant_id = $1)`,
		`DELETE FROM api_keys            WHERE project_id IN (SELECT id FROM projects WHERE tenant_id = $1)`,
		`DELETE FROM projects            WHERE tenant_id = $1`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(ctx, q, tenantID); err != nil {
			t.Logf("wipe: %q: %v", q, err)
		}
	}
}
