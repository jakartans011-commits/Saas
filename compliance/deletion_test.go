package compliance

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	bolt "go.etcd.io/bbolt"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/auth"
	"trailseal/project"
	"trailseal/tenant"
)

func openTestDB(t *testing.T) (*bolt.DB, func()) {
	dbPath := filepath.Join(t.TempDir(), "compliance_test.db")
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return db, func() { db.Close() }
}

// noopDBQuerier — fake project.DBQuerier / apikey.DBQuerier untuk test
// compliance package, yang tidak butuh Postgres asli. Test purge di sini
// tidak pernah membuat project/API key sungguhan, jadi cukup no-op yang
// tidak error — cuma memastikan PurgeTenant bisa memanggil
// DeleteAllForTenant tanpa panic.
type noopDBQuerier struct{}

func (noopDBQuerier) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return nil
}
func (noopDBQuerier) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (noopDBQuerier) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

// newTestPurgeStores — project/apikey/audit store untuk test PurgeTenant,
// dipakai bareng oleh deletion_test.go, deletion_extra_test.go, dan
// deletion_grace_period_test.go supaya tidak duplikasi.
func newTestPurgeStores(db *bolt.DB) (*project.PostgresStore, *apikey.PostgresStore, *audit.BoltAuditStore) {
	projectStore := project.NewPostgresStore(noopDBQuerier{})
	apiKeyStore := apikey.NewPostgresStore(noopDBQuerier{})
	auditStore, _ := audit.NewBoltAuditStore(db)
	return projectStore, apiKeyStore, auditStore
}

func TestSoftDeleteTenant(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	// Create tenant
	tn := &tenant.Tenant{ID: "test-tenant", Name: "Test Tenant", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// Create user
	user := &auth.User{ID: "user-1", TenantID: "test-tenant", Email: "test@test.test", Status: auth.UserStatusActive}
	if err := userStore.Put(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Soft delete
	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "test-tenant"); err != nil {
		t.Fatalf("SoftDeleteTenant failed: %v", err)
	}

	// Verify tenant soft-deleted
	got, err := tenantStore.GetIncludingDeleted("test-tenant")
	if err != nil {
		t.Fatalf("GetIncludingDeleted failed: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatal("expected DeletedAt to be set")
	}

	// Verify Get() returns not found
	_, err = tenantStore.Get("test-tenant")
	if err == nil {
		t.Fatal("expected Get to return error for soft-deleted tenant")
	}
}

func TestRestoreTenant(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	// Create tenant
	tn := &tenant.Tenant{ID: "test-tenant", Name: "Test Tenant", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// Soft delete
	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "test-tenant"); err != nil {
		t.Fatalf("SoftDeleteTenant failed: %v", err)
	}

	// Restore
	if err := RestoreTenant(tenantStore, userStore, agentStore, "test-tenant"); err != nil {
		t.Fatalf("RestoreTenant failed: %v", err)
	}

	// Verify tenant restored
	got, err := tenantStore.Get("test-tenant")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatal("expected DeletedAt to be nil after restore")
	}
}

func TestPurgeTenant(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	// Create tenant
	tn := &tenant.Tenant{ID: "test-tenant", Name: "Test Tenant", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// Soft delete
	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "test-tenant"); err != nil {
		t.Fatalf("SoftDeleteTenant failed: %v", err)
	}

	// Set DeletedAt to past (bypass grace period)
	tenant, _ := tenantStore.GetIncludingDeleted("test-tenant")
	past := time.Now().Add(-40 * 24 * time.Hour)
	tenant.DeletedAt = &past
	if err := tenantStore.Put(tenant); err != nil {
		t.Fatalf("failed to update DeletedAt: %v", err)
	}

	// Purge
	config := &DeletionConfig{GracePeriod: 30 * 24 * time.Hour}
	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)
	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "test-tenant", config); err != nil {
		t.Fatalf("PurgeTenant failed: %v", err)
	}

	// Verify tenant hard-deleted
	_, err := tenantStore.GetIncludingDeleted("test-tenant")
	if err == nil {
		t.Fatal("expected error after purge")
	}
}

func TestPurgeTenant_GracePeriod(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	// Create tenant
	tn := &tenant.Tenant{ID: "test-tenant", Name: "Test Tenant", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// Soft delete
	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "test-tenant"); err != nil {
		t.Fatalf("SoftDeleteTenant failed: %v", err)
	}

	// Purge immediately (should fail due to grace period)
	config := &DeletionConfig{GracePeriod: 30 * 24 * time.Hour}
	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)
	err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "test-tenant", config)
	if err == nil {
		t.Fatal("expected error due to grace period, got nil")
	}
}