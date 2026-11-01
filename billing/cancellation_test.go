package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/tenant"
)

// ============================================================
// Cancellation — item #1 (Tingkat 1)
//
// Covers what happens when a Stripe subscription is canceled:
//   - directly, at the business-logic level (handleSubscriptionDeleted)
//   - end-to-end, through the real HTTP handler (WebhookHandler),
//     including Stripe signature verification and webhook
//     idempotency (duplicate delivery).
//
// Business rule under test: a canceled subscription must downgrade
// the tenant to the Free plan (not leave it on its last paid tier),
// and that downgrade must propagate to every active project's
// entitlements (event limit / retention).
// ============================================================

func newTestService(t *testing.T, tenantStore tenant.TenantStore, projectStore ProjectPlanApplier, webhookStore WebhookEventClaimer) *Service {
	t.Helper()
	plans := NewRegistry("price_pro_test", "price_business_test")
	return NewService(
		tenantStore,
		projectStore,
		webhookStore,
		plans,
		"whsec_test_secret",
		"https://app.example.com/billing/success",
		"https://app.example.com/billing/cancel",
	)
}

func seedTenant(t *testing.T, store tenant.TenantStore, tt *tenant.Tenant) {
	t.Helper()
	if err := store.Put(tt); err != nil {
		t.Fatalf("failed to seed tenant: %v", err)
	}
}

// ---- business logic level ----

func TestHandleSubscriptionDeleted_DowngradesTenantToFree(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                   "acme-corp",
		Name:                 "Acme Corp",
		Status:               tenant.TenantStatusActive,
		StripeCustomerID:     "cus_123",
		StripeSubscriptionID: "sub_123",
		PlanTier:             "business",
		SubscriptionStatus:   "active",
	})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	err := svc.handleSubscriptionDeleted(context.Background(), subscriptionPayload{
		ID:       "sub_123",
		Status:   "canceled",
		Metadata: map[string]string{"tenant_id": "acme-corp"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := tenantStore.Get("acme-corp")
	if err != nil {
		t.Fatalf("failed to reload tenant: %v", err)
	}

	if got.PlanTier != string(PlanFree) {
		t.Errorf("expected plan tier %q, got %q", PlanFree, got.PlanTier)
	}
	if got.SubscriptionStatus != "canceled" {
		t.Errorf("expected subscription status %q, got %q", "canceled", got.SubscriptionStatus)
	}

	call, ok := projectStore.lastCall()
	if !ok {
		t.Fatal("expected ApplyPlanToTenant to be called")
	}
	free := NewRegistry("price_pro_test", "price_business_test").Free()
	if call.tenantID != "acme-corp" {
		t.Errorf("expected tenantID %q, got %q", "acme-corp", call.tenantID)
	}
	if call.monthlyEventLimit != free.MonthlyEventLimit {
		t.Errorf("expected monthly event limit %d, got %d", free.MonthlyEventLimit, call.monthlyEventLimit)
	}
	if call.retentionDays != free.RetentionDays {
		t.Errorf("expected retention days %d, got %d", free.RetentionDays, call.retentionDays)
	}
}

func TestHandleSubscriptionDeleted_MissingTenantIDMetadata(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	err := svc.handleSubscriptionDeleted(context.Background(), subscriptionPayload{
		ID:     "sub_123",
		Status: "canceled",
		// Metadata intentionally omitted.
	})
	if err == nil {
		t.Fatal("expected error when tenant_id metadata is missing")
	}
}

func TestHandleSubscriptionDeleted_UnknownTenant(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	err := svc.handleSubscriptionDeleted(context.Background(), subscriptionPayload{
		ID:       "sub_ghost",
		Status:   "canceled",
		Metadata: map[string]string{"tenant_id": "does-not-exist"},
	})
	if err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

// TestHandleSubscriptionDeleted_ProjectPropagationFailure documents a
// known ordering gap rather than asserting "correct" behavior:
// tenantStore.Put happens before projectStore.ApplyPlanToTenant, so a
// failure in the latter leaves the tenant record downgraded to Free
// while its projects still carry the old (paid) entitlements. There
// is no cross-store transaction. If this ever needs to be fixed
// (e.g. via a saga/outbox or reordering + compensating action), this
// test is the one to update.
func TestHandleSubscriptionDeleted_ProjectPropagationFailure(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                 "acme-corp",
		PlanTier:           "business",
		SubscriptionStatus: "active",
	})
	projectStore := &fakeProjectStore{err: errors.New("db unreachable")}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	err := svc.handleSubscriptionDeleted(context.Background(), subscriptionPayload{
		ID:       "sub_123",
		Status:   "canceled",
		Metadata: map[string]string{"tenant_id": "acme-corp"},
	})
	if err == nil {
		t.Fatal("expected error to propagate from ApplyPlanToTenant")
	}

	// Tenant was already downgraded before the failure — this is the
	// gap being documented, not the desired end state.
	got, _ := tenantStore.Get("acme-corp")
	if got.PlanTier != string(PlanFree) {
		t.Errorf("expected tenant to already show plan tier %q despite propagation failure, got %q", PlanFree, got.PlanTier)
	}
}

// ---- HTTP handler level (real Stripe signature, real fiber route) ----

func newSubscriptionDeletedPayload(eventID, tenantID string) []byte {
	env := map[string]interface{}{
		"id":   eventID,
		"type": "customer.subscription.deleted",
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"id":       "sub_123",
				"status":   "canceled",
				"metadata": map[string]string{"tenant_id": tenantID},
				"items":    map[string]interface{}{"data": []interface{}{}},
			},
		},
	}
	body, _ := json.Marshal(env)
	return body
}

func TestWebhookHandler_SubscriptionDeleted_DowngradesTenant(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                 "acme-corp",
		PlanTier:           "pro",
		SubscriptionStatus: "active",
	})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionDeletedPayload("evt_cancel_1", "acme-corp")
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
	if got.PlanTier != string(PlanFree) {
		t.Errorf("expected tenant downgraded to %q, got %q", PlanFree, got.PlanTier)
	}
	if projectStore.callCount() != 1 {
		t.Errorf("expected exactly 1 ApplyPlanToTenant call, got %d", projectStore.callCount())
	}
}

func TestWebhookHandler_SubscriptionDeleted_InvalidSignatureRejected(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", PlanTier: "pro"})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionDeletedPayload("evt_cancel_bad_sig", "acme-corp")

	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid signature, got %d", resp.StatusCode)
	}

	// Tenant must be untouched — an unverified request must never
	// mutate billing state.
	got, _ := tenantStore.Get("acme-corp")
	if got.PlanTier != "pro" {
		t.Errorf("expected tenant plan tier unchanged at %q, got %q", "pro", got.PlanTier)
	}
}

// TestWebhookHandler_SubscriptionDeleted_DuplicateDeliveryIsIdempotent
// simulates Stripe retrying/duplicating the same event ID: the second
// delivery must be recognized as already-claimed and must NOT run the
// downgrade logic again.
func TestWebhookHandler_SubscriptionDeleted_DuplicateDeliveryIsIdempotent(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", PlanTier: "pro", SubscriptionStatus: "active"})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionDeletedPayload("evt_cancel_dup", "acme-corp")

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
		Received  bool `json:"received"`
		Duplicate bool `json:"duplicate"`
	}
	if err := json.NewDecoder(second.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode duplicate response: %v", err)
	}
	if !body.Duplicate {
		t.Error("expected duplicate:true on second delivery")
	}

	if projectStore.callCount() != 1 {
		t.Errorf("expected downgrade logic to run exactly once despite 2 deliveries, got %d calls", projectStore.callCount())
	}
}
