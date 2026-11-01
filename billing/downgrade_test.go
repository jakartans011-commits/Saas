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
// Downgrade — item #2 (Tingkat 1)
//
// A "downgrade" arrives as customer.subscription.updated with a new
// (lower-tier) Stripe price on the subscription's line item — Stripe
// does not send a distinct "downgrade" event type. handleSubscriptionUpdated
// resolves whatever price is currently active and applies it, so the
// same code path handles upgrades and downgrades symmetrically; the
// tests below cover both directions plus the fail-closed behavior
// for unrecognized prices.
// ============================================================

// ---- business logic level ----

func TestHandleSubscriptionUpdated_DowngradesBusinessToPro(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                   "acme-corp",
		StripeSubscriptionID: "sub_123",
		PlanTier:             "business",
		SubscriptionStatus:   "active",
	})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	sub := subscriptionPayload{
		ID:       "sub_123",
		Status:   "active",
		Metadata: map[string]string{"tenant_id": "acme-corp"},
	}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{
		{Price: struct {
			ID string `json:"id"`
		}{ID: "price_pro_test"}},
	}

	if err := svc.handleSubscriptionUpdated(context.Background(), sub); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := tenantStore.Get("acme-corp")
	if err != nil {
		t.Fatalf("failed to reload tenant: %v", err)
	}
	if got.PlanTier != string(PlanPro) {
		t.Errorf("expected plan tier %q after downgrade, got %q", PlanPro, got.PlanTier)
	}
	if got.SubscriptionStatus != "active" {
		t.Errorf("expected subscription status %q, got %q", "active", got.SubscriptionStatus)
	}

	call, ok := projectStore.lastCall()
	if !ok {
		t.Fatal("expected ApplyPlanToTenant to be called")
	}
	pro, _ := NewRegistry("price_pro_test", "price_business_test").Get(PlanPro)
	if call.monthlyEventLimit != pro.MonthlyEventLimit || call.retentionDays != pro.RetentionDays {
		t.Errorf("expected pro entitlements (%d, %d), got (%d, %d)",
			pro.MonthlyEventLimit, pro.RetentionDays, call.monthlyEventLimit, call.retentionDays)
	}
}

// TestHandleSubscriptionUpdated_UpgradesProToBusiness proves the same
// handler is symmetric: it is not "downgrade-only" logic, it just
// applies whatever plan the new price resolves to.
func TestHandleSubscriptionUpdated_UpgradesProToBusiness(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                   "acme-corp",
		StripeSubscriptionID: "sub_123",
		PlanTier:             "pro",
		SubscriptionStatus:   "active",
	})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	sub := subscriptionPayload{ID: "sub_123", Status: "active", Metadata: map[string]string{"tenant_id": "acme-corp"}}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{
		{Price: struct {
			ID string `json:"id"`
		}{ID: "price_business_test"}},
	}

	if err := svc.handleSubscriptionUpdated(context.Background(), sub); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tenantStore.Get("acme-corp")
	if got.PlanTier != string(PlanBusiness) {
		t.Errorf("expected plan tier %q after upgrade, got %q", PlanBusiness, got.PlanTier)
	}
}

func TestHandleSubscriptionUpdated_UnknownPriceIDFailsClosedWithoutMutatingTenant(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                 "acme-corp",
		PlanTier:           "business",
		SubscriptionStatus: "active",
	})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	sub := subscriptionPayload{ID: "sub_123", Status: "active", Metadata: map[string]string{"tenant_id": "acme-corp"}}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{
		{Price: struct {
			ID string `json:"id"`
		}{ID: "price_attacker_controlled"}},
	}

	err := svc.handleSubscriptionUpdated(context.Background(), sub)
	if err == nil {
		t.Fatal("expected error for unknown price ID")
	}

	// Fail-closed: tenant plan must be left exactly as it was.
	got, _ := tenantStore.Get("acme-corp")
	if got.PlanTier != "business" {
		t.Errorf("expected tenant plan tier untouched at %q, got %q", "business", got.PlanTier)
	}
	if projectStore.callCount() != 0 {
		t.Errorf("expected no entitlement propagation for an unresolved plan, got %d calls", projectStore.callCount())
	}
}

func TestHandleSubscriptionUpdated_NoItemsIsRejected(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", PlanTier: "business"})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	sub := subscriptionPayload{ID: "sub_123", Status: "active", Metadata: map[string]string{"tenant_id": "acme-corp"}}
	// sub.Items.Data left empty on purpose.

	if err := svc.handleSubscriptionUpdated(context.Background(), sub); err == nil {
		t.Fatal("expected error when subscription has no line items")
	}
}

func TestHandleSubscriptionUpdated_MissingTenantIDMetadata(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	sub := subscriptionPayload{ID: "sub_123", Status: "active"}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{
		{Price: struct {
			ID string `json:"id"`
		}{ID: "price_pro_test"}},
	}

	if err := svc.handleSubscriptionUpdated(context.Background(), sub); err == nil {
		t.Fatal("expected error when tenant_id metadata is missing")
	}
}

// ---- HTTP handler level ----

func newSubscriptionUpdatedPayload(eventID, tenantID, priceID string) []byte {
	env := map[string]interface{}{
		"id":   eventID,
		"type": "customer.subscription.updated",
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"id":       "sub_123",
				"status":   "active",
				"metadata": map[string]string{"tenant_id": tenantID},
				"items": map[string]interface{}{
					"data": []map[string]interface{}{
						{"price": map[string]interface{}{"id": priceID}},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(env)
	return body
}

func TestWebhookHandler_SubscriptionUpdated_DowngradesTenant(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                 "acme-corp",
		PlanTier:           "business",
		SubscriptionStatus: "active",
	})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionUpdatedPayload("evt_downgrade_1", "acme-corp", "price_pro_test")
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
	if got.PlanTier != string(PlanPro) {
		t.Errorf("expected tenant downgraded to %q, got %q", PlanPro, got.PlanTier)
	}
}

func TestWebhookHandler_SubscriptionUpdated_UnknownPriceReturns500AndTenantUnchanged(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                 "acme-corp",
		PlanTier:           "business",
		SubscriptionStatus: "active",
	})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionUpdatedPayload("evt_downgrade_bad_price", "acme-corp", "price_does_not_exist")
	sig := signStripePayload(svc.webhookSecret, payload, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", sig)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for an unresolvable price, got %d", resp.StatusCode)
	}

	got, _ := tenantStore.Get("acme-corp")
	if got.PlanTier != "business" {
		t.Errorf("expected tenant plan tier untouched at %q, got %q", "business", got.PlanTier)
	}
}
