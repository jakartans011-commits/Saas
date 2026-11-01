// billing/checkout_session_test.go
package billing

import (
	"testing"

	"trailseal/tenant"
)

// CreateCheckoutSession — covers the two failure branches reachable
// without contacting Stripe. The success path (session.New) needs a
// live Stripe test-mode key; see the note in checkout_completed_test.go.

func TestCreateCheckoutSession_UnknownPlanTier(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", Status: tenant.TenantStatusActive})

	plans := NewRegistry("price_pro", "price_business")
	svc := NewService(tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore(), plans, "whsec_test", "https://example.com/ok", "https://example.com/cancel")

	_, err := svc.CreateCheckoutSession("acme-corp", PlanTier("not_a_real_tier"))
	if err != errInvalidPlan {
		t.Fatalf("expected errInvalidPlan, got %v", err)
	}
}

func TestCreateCheckoutSession_PlanWithoutStripePriceID(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", Status: tenant.TenantStatusActive})

	// Empty env price IDs -> Pro/Business never get a StripePriceID,
	// and Free never has one by design.
	plans := NewRegistry("", "")
	svc := NewService(tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore(), plans, "whsec_test", "https://example.com/ok", "https://example.com/cancel")

	_, err := svc.CreateCheckoutSession("acme-corp", PlanFree)
	if err != errInvalidPlan {
		t.Fatalf("expected errInvalidPlan for a plan with no Stripe price ID, got %v", err)
	}
}

func TestCreateCheckoutSession_UnknownTenant(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()

	plans := NewRegistry("price_pro", "price_business")
	svc := NewService(tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore(), plans, "whsec_test", "https://example.com/ok", "https://example.com/cancel")

	_, err := svc.CreateCheckoutSession("does-not-exist", PlanPro)
	if err != errTenantNotFoundForCheckout {
		t.Fatalf("expected errTenantNotFoundForCheckout, got %v", err)
	}
}
