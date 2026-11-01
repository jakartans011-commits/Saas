package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/tenant"
)

// ============================================================
// Checkout completed â€” handleCheckoutCompleted was previously
// completely untested (0% coverage). This is the handler that
// records a tenant's Stripe customer/subscription IDs the first
// time they pay â€” a bug here means a paying customer's tenant
// never gets linked to their Stripe subscription.
// ============================================================

// ---- business logic level ----

func TestHandleCheckoutCompleted_SetsCustomerAndSubscriptionIDs(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:     "acme-corp",
		Name:   "Acme Corp",
		Status: tenant.TenantStatusActive,
	})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	err := svc.handleCheckoutCompleted(context.Background(), checkoutSessionPayload{
		ClientReferenceID: "acme-corp",
		Customer:          "cus_new123",
		Subscription:      "sub_new123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := tenantStore.Get("acme-corp")
	if err != nil {
		t.Fatalf("failed to reload tenant: %v", err)
	}
	if got.StripeCustomerID != "cus_new123" {
		t.Errorf("expected StripeCustomerID %q, got %q", "cus_new123", got.StripeCustomerID)
	}
	if got.StripeSubscriptionID != "sub_new123" {
		t.Errorf("expected StripeSubscriptionID %q, got %q", "sub_new123", got.StripeSubscriptionID)
	}
}

func TestHandleCheckoutCompleted_MissingClientReferenceID(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	err := svc.handleCheckoutCompleted(context.Background(), checkoutSessionPayload{
		Customer:     "cus_orphan",
		Subscription: "sub_orphan",
		// ClientReferenceID intentionally omitted.
	})
	if err == nil {
		t.Fatal("expected error when client_reference_id is missing")
	}
}

func TestHandleCheckoutCompleted_UnknownTenant(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	err := svc.handleCheckoutCompleted(context.Background(), checkoutSessionPayload{
		ClientReferenceID: "does-not-exist",
		Customer:          "cus_ghost",
	})
	if err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

// TestHandleCheckoutCompleted_EmptyFieldsDoNotOverwriteExisting documents
// the actual behavior: sess.Customer / sess.Subscription only overwrite
// the tenant's IDs when non-empty. A checkout.session.completed payload
// that (for whatever reason) omits customer/subscription must not wipe
// out IDs the tenant already had.
func TestHandleCheckoutCompleted_EmptyFieldsDoNotOverwriteExisting(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                   "acme-corp",
		StripeCustomerID:     "cus_existing",
		StripeSubscriptionID: "sub_existing",
	})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	err := svc.handleCheckoutCompleted(context.Background(), checkoutSessionPayload{
		ClientReferenceID: "acme-corp",
		// Customer / Subscription intentionally left empty.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tenantStore.Get("acme-corp")
	if got.StripeCustomerID != "cus_existing" {
		t.Errorf("expected StripeCustomerID to remain %q, got %q", "cus_existing", got.StripeCustomerID)
	}
	if got.StripeSubscriptionID != "sub_existing" {
		t.Errorf("expected StripeSubscriptionID to remain %q, got %q", "sub_existing", got.StripeSubscriptionID)
	}
}

// ---- HTTP handler level (real Stripe signature, real fiber route) ----

func newCheckoutCompletedPayload(eventID, tenantID, customerID, subscriptionID string) []byte {
	env := map[string]interface{}{
		"id":   eventID,
		"type": "checkout.session.completed",
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"client_reference_id": tenantID,
				"customer":            customerID,
				"subscription":        subscriptionID,
			},
		},
	}
	body, _ := json.Marshal(env)
	return body
}

func TestWebhookHandler_CheckoutCompleted_LinksStripeIDs(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:     "acme-corp",
		Status: tenant.TenantStatusActive,
	})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newCheckoutCompletedPayload("evt_checkout_1", "acme-corp", "cus_abc", "sub_abc")
	sig := signStripePayload(svc.webhookSecret, payload, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", sig)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, err := tenantStore.Get("acme-corp")
	if err != nil {
		t.Fatalf("failed to reload tenant: %v", err)
	}
	if got.StripeCustomerID != "cus_abc" {
		t.Errorf("expected StripeCustomerID %q, got %q", "cus_abc", got.StripeCustomerID)
	}
	if got.StripeSubscriptionID != "sub_abc" {
		t.Errorf("expected StripeSubscriptionID %q, got %q", "sub_abc", got.StripeSubscriptionID)
	}
}

func TestWebhookHandler_CheckoutCompleted_UnknownTenantReturns500(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newCheckoutCompletedPayload("evt_checkout_ghost", "does-not-exist", "cus_ghost", "sub_ghost")
	sig := signStripePayload(svc.webhookSecret, payload, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", sig)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unknown tenant, got %d", resp.StatusCode)
	}
}

// TestWebhookHandler_CheckoutCompleted_DuplicateDeliveryIsIdempotent mirrors
// the equivalent test in cancellation_test.go for subscription.deleted â€”
// checkout.session.completed must be just as protected against Stripe's
// at-least-once delivery guarantee.
func TestWebhookHandler_CheckoutCompleted_DuplicateDeliveryIsIdempotent(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", Status: tenant.TenantStatusActive})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newCheckoutCompletedPayload("evt_checkout_dup", "acme-corp", "cus_dup", "sub_dup")

	send := func() *http.Response {
		sig := signStripePayload(svc.webhookSecret, payload, time.Now())
		req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Stripe-Signature", sig)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return resp
	}

	first := send()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on first delivery, got %d", first.StatusCode)
	}

	second := send()
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on duplicate delivery, got %d", second.StatusCode)
	}
	var body struct {
		Duplicate bool `json:"duplicate"`
	}
	if err := json.NewDecoder(second.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode duplicate response: %v", err)
	}
	if !body.Duplicate {
		t.Error("expected duplicate:true on second delivery")
	}
}

// ============================================================
// CreateCheckoutSession / CreateCheckoutHandler â€” PARTIAL COVERAGE ONLY.
//
// session.New() is a package-level call into the real stripe-go SDK
// (not injected via an interface), so exercising the success path
// here would require a live Stripe test-mode secret key and network
// access. These tests cover only the two failure paths that return
// BEFORE session.New() is ever called â€” errInvalidPlan and
// errTenantNotFoundForCheckout â€” which need no network access at all.
//
// The success path (a valid plan + valid tenant, actually reaching
// session.New) remains untested here. If a Stripe test-mode secret
// key becomes available in the test environment, add a case that
// sets STRIPE_KEY / stripe.Key and asserts CreateCheckoutSession
// returns a non-empty URL.
// ============================================================

func TestCreateCheckoutHandler_InvalidPlan_Returns400(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", Status: tenant.TenantStatusActive})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/tenants/:id/checkout", svc.CreateCheckoutHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/tenants/acme-corp/checkout", strings.NewReader(`{"plan":"not_a_real_plan"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid plan, got %d", resp.StatusCode)
	}
}

func TestCreateCheckoutHandler_UnknownTenant_Returns404(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/tenants/:id/checkout", svc.CreateCheckoutHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/tenants/does-not-exist/checkout", strings.NewReader(`{"plan":"pro"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown tenant, got %d", resp.StatusCode)
	}
}

func TestCreateCheckoutHandler_InvalidBody_Returns400(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", Status: tenant.TenantStatusActive})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/tenants/:id/checkout", svc.CreateCheckoutHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/tenants/acme-corp/checkout", strings.NewReader(`{not valid json`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d", resp.StatusCode)
	}
}
