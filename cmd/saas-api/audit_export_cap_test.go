package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
)

// ============================================================
// P2-2 jalur B: GET /api/tenants/:id/audit/export dengan cap plan.
// Tenant baru = plan Free (PlanTier kosong, fail closed) = cap 100.
//
//   go test ./cmd/saas-api -run AuditExportCap -v
//
// Butuh TRAILSEAL_TEST_POSTGRES_URL (store audit produksi = Postgres).
// ============================================================

func auditCapUUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand failed: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type auditExportResp struct {
	TotalEvents int `json:"total_events"`
	Events      []struct {
		Action string `json:"action"`
	} `json:"events"`
}

func TestAuditExportCap_FreePlanGetsNewest100(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	const (
		tenantID = "audit-cap-tenant"
		email    = "owner@audit-cap.test"
		password = "Passw0rd!Passw0rd"
		total    = 150
	)

	env.bootstrapTenantOwner(t, tenantID, "Audit Cap Tenant", email, password)

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

	doExport := func(t *testing.T, query string) (*auditExportResp, string, string) {
		t.Helper()
		req := httptest.NewRequest(fiber.MethodGet, "/api/tenants/"+tenantID+"/audit/export"+query, nil)
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
		return &out, resp.Header.Get("X-Export-Capped"), resp.Header.Get("X-Export-Plan-Cap")
	}

	t.Run("limit 5000 is cut to 100 newest plus headers", func(t *testing.T) {
		out, capped, planCap := doExport(t, "?limit=5000")

		if len(out.Events) != 100 || out.TotalEvents != 100 {
			t.Fatalf("expected 100 events, got events=%d total_events=%d", len(out.Events), out.TotalEvents)
		}
		if capped != "true" {
			t.Errorf("expected X-Export-Capped=true, got %q", capped)
		}
		if planCap != "100" {
			t.Errorf("expected X-Export-Plan-Cap=100, got %q", planCap)
		}

		seen := map[string]bool{}
		for _, e := range out.Events {
			seen[e.Action] = true
		}
		if !seen["cap.test.150"] || !seen["cap.test.051"] {
			t.Errorf("newest range 051..150 must be present")
		}
		if seen["cap.test.050"] || seen["cap.test.001"] {
			t.Errorf("older events (<=050) must not be exported on Free")
		}
	})

	t.Run("limit below cap is respected", func(t *testing.T) {
		out, capped, _ := doExport(t, "?limit=5")
		if len(out.Events) != 5 {
			t.Fatalf("expected 5 events, got %d", len(out.Events))
		}
		if capped != "true" {
			t.Errorf("expected X-Export-Capped=true, got %q", capped)
		}
	})
}
