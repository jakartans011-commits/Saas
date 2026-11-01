package billing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/tenant"
)

// CreateCheckoutSessionInterval: cabang yang bisa diuji tanpa
// menghubungi Stripe (sukses session.New butuh key test-mode).

func newIntervalTestService(t *testing.T, plans *Registry, seed bool) *Service {
	t.Helper()
	tenantStore := tenant.NewInMemoryTenantStore()
	if seed {
		seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", Status: tenant.TenantStatusActive})
	}
	return NewService(tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore(), plans, "whsec_test", "https://example.com/ok", "https://example.com/cancel")
}

func TestCreateCheckoutSessionInterval_AnnualNotConfigured(t *testing.T) {
	svc := newIntervalTestService(t, NewRegistry("price_pro", "price_business"), true)

	_, err := svc.CreateCheckoutSessionInterval("acme-corp", PlanPro, IntervalAnnual)
	if err != errIntervalNotAvailable {
		t.Fatalf("expected errIntervalNotAvailable, got %v", err)
	}
}

func TestCreateCheckoutSessionInterval_FreeAndUnknownStayInvalidPlan(t *testing.T) {
	svc := newIntervalTestService(t, NewRegistry("price_pro", "price_business"), true)

	for _, tier := range []PlanTier{PlanFree, PlanTier("not_a_real_tier")} {
		if _, err := svc.CreateCheckoutSessionInterval("acme-corp", tier, IntervalAnnual); err != errInvalidPlan {
			t.Errorf("tier %q annual: expected errInvalidPlan, got %v", tier, err)
		}
	}
}

func TestCreateCheckoutSessionInterval_AnnualConfigured_UnknownTenant(t *testing.T) {
	plans := NewRegistry("price_pro", "price_business")
	if err := plans.SetAnnualPriceID(PlanPro, "price_pro_annual"); err != nil {
		t.Fatalf("SetAnnualPriceID failed: %v", err)
	}
	svc := newIntervalTestService(t, plans, false)

	// Plan lolos validasi price (tahunan terkonfigurasi), jadi yang
	// gagal adalah tenant, sebelum Stripe dihubungi.
	_, err := svc.CreateCheckoutSessionInterval("does-not-exist", PlanPro, IntervalAnnual)
	if err != errTenantNotFoundForCheckout {
		t.Fatalf("expected errTenantNotFoundForCheckout, got %v", err)
	}
}

func postCheckout(t *testing.T, svc *Service, body string) (int, string) {
	t.Helper()

	app := fiber.New()
	app.Post("/api/billing/tenants/:id/checkout", svc.CreateCheckoutHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/tenants/acme-corp/checkout", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Error
}

func TestCreateCheckoutHandler_InvalidInterval_Returns400(t *testing.T) {
	svc := newIntervalTestService(t, NewRegistry("price_pro", "price_business"), true)

	status, code := postCheckout(t, svc, `{"plan":"pro","interval":"weekly"}`)
	if status != http.StatusBadRequest || code != "invalid_interval" {
		t.Fatalf("expected 400 invalid_interval, got %d %q", status, code)
	}
}

func TestCreateCheckoutHandler_AnnualNotConfigured_Returns400(t *testing.T) {
	svc := newIntervalTestService(t, NewRegistry("price_pro", "price_business"), true)

	status, code := postCheckout(t, svc, `{"plan":"pro","interval":"annual"}`)
	if status != http.StatusBadRequest || code != "interval_not_available" {
		t.Fatalf("expected 400 interval_not_available, got %d %q", status, code)
	}
}

func TestCreateCheckoutHandler_AnnualForFreePlan_StaysInvalidPlan(t *testing.T) {
	svc := newIntervalTestService(t, NewRegistry("price_pro", "price_business"), true)

	status, code := postCheckout(t, svc, `{"plan":"free","interval":"annual"}`)
	if status != http.StatusBadRequest || code != "invalid_plan" {
		t.Fatalf("expected 400 invalid_plan, got %d %q", status, code)
	}
}