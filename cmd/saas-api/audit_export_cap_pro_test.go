package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
)

// Pro (AuditExportMaxEvents = 0) harus export penuh tanpa header cap.
// Memakai helper dari audit_export_cap_test.go (paket yang sama).
//
//   go test ./cmd/saas-api -run AuditExportCap -v

func TestAuditExportCap_ProPlanNotCapped(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	const (
		tenantID = "audit-cap-pro-tenant"
		email    = "owner@audit-cap-pro.test"
		password = "Passw0rd!Passw0rd"
		total    = 150
	)

	env.bootstrapTenantOwner(t, tenantID, "Audit Cap Pro Tenant", email, password)

	tn, err := env.tenantStore.Get(tenantID)
	if err != nil {
		t.Fatalf("tenantStore.Get failed: %v", err)
	}
	tn.PlanTier = "pro"
	if err := env.tenantStore.Put(tn); err != nil {
		t.Fatalf("tenantStore.Put failed: %v", err)
	}

	auditStore, err := audit.NewPostgresAuditStore(env.pgPool)
	if err != nil {
		t.Fatalf("NewPostgresAuditStore failed: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.DeleteAllForTenant(tenantID) })

	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	for i := 1; i <= total; i++ {
		ev := &audit.AuditEvent{
			ID:        auditCapUUID(t),
			TenantID:  tenantID,
			UserID:    "tester",
			Action:    fmt.Sprintf("cap.test.%03d", i),
			Resource:  "audit-cap",
			Outcome:   audit.Outcome("success"),
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}
		if err := auditStore.Save(ev); err != nil {
			t.Fatalf("seed audit event %d failed: %v", i, err)
		}
	}

	token := env.login(t, email, password)

	req := httptest.NewRequest(fiber.MethodGet, "/api/tenants/"+tenantID+"/audit/export?limit=1000", nil)
	for k, v := range bearer(token) {
		req.Header.Set(k, v)
	}
	resp, err := env.app.Test(req, 10000)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out auditExportResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode export: %v", err)
	}

	if len(out.Events) != total || out.TotalEvents != total {
		t.Fatalf("expected %d events on Pro, got events=%d total_events=%d", total, len(out.Events), out.TotalEvents)
	}
	if h := resp.Header.Get("X-Export-Capped"); h != "" {
		t.Errorf("expected no X-Export-Capped header on Pro, got %q", h)
	}
	if h := resp.Header.Get("X-Export-Plan-Cap"); h != "" {
		t.Errorf("expected no X-Export-Plan-Cap header on Pro, got %q", h)
	}
}
