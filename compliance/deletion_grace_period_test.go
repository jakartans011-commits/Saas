package compliance

import (
	"context"
	"testing"
	"time"

	"trailseal/agent"
	"trailseal/auth"
	"trailseal/tenant"
)

func TestPurgeTenant_FreeTier_30DayGracePeriod(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "free-tenant", PlanTier: "free"}
	_ = tenantStore.Put(tn)
	_ = SoftDeleteTenant(tenantStore, userStore, agentStore, "free-tenant")

	// 29 hari — harus ditolak
	got, _ := tenantStore.GetIncludingDeleted("free-tenant")
	past29 := time.Now().Add(-29 * 24 * time.Hour)
	got.DeletedAt = &past29
	_ = tenantStore.Put(got)

	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)

	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "free-tenant", nil); err == nil {
		t.Fatal("expected grace period error at day 29 for Free tier")
	}

	// 31 hari — harus lolos
	past31 := time.Now().Add(-31 * 24 * time.Hour)
	got.DeletedAt = &past31
	_ = tenantStore.Put(got)

	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "free-tenant", nil); err != nil {
		t.Fatalf("expected purge to succeed at day 31 for Free tier: %v", err)
	}
}

func TestPurgeTenant_BusinessTier_60DayGracePeriod(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "biz-tenant", PlanTier: "business"}
	_ = tenantStore.Put(tn)
	_ = SoftDeleteTenant(tenantStore, userStore, agentStore, "biz-tenant")

	// 31 hari — masih dalam grace period Business (60 hari), harus ditolak
	got, _ := tenantStore.GetIncludingDeleted("biz-tenant")
	past31 := time.Now().Add(-31 * 24 * time.Hour)
	got.DeletedAt = &past31
	_ = tenantStore.Put(got)

	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)

	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "biz-tenant", nil); err == nil {
		t.Fatal("expected grace period error at day 31 for Business tier")
	}

	// 61 hari — harus lolos
	past61 := time.Now().Add(-61 * 24 * time.Hour)
	got.DeletedAt = &past61
	_ = tenantStore.Put(got)

	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "biz-tenant", nil); err != nil {
		t.Fatalf("expected purge to succeed at day 61 for Business tier: %v", err)
	}
}

func TestPurgeTenant_ProTier_30DayGracePeriod(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "pro-tenant", PlanTier: "pro"}
	_ = tenantStore.Put(tn)
	_ = SoftDeleteTenant(tenantStore, userStore, agentStore, "pro-tenant")

	// 29 hari — harus ditolak
	got, _ := tenantStore.GetIncludingDeleted("pro-tenant")
	past29 := time.Now().Add(-29 * 24 * time.Hour)
	got.DeletedAt = &past29
	_ = tenantStore.Put(got)

	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)

	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "pro-tenant", nil); err == nil {
		t.Fatal("expected grace period error at day 29 for Pro tier")
	}

	// 31 hari — harus lolos (sama dengan Free, bukan 60 hari Business)
	past31 := time.Now().Add(-31 * 24 * time.Hour)
	got.DeletedAt = &past31
	_ = tenantStore.Put(got)

	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "pro-tenant", nil); err != nil {
		t.Fatalf("expected purge to succeed at day 31 for Pro tier: %v", err)
	}
}