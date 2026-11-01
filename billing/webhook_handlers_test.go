package billing

import (
	"context"
	"testing"
	"time"

	"trailseal/tenant"
)

// ============================================================
// Shared test setup — mirrors the pattern in service_test_helpers_test.go
// (fakeProjectStore, fakeWebhookEventStore). No new fakes introduced:
// tenant.InMemoryTenantStore already implements both TenantStore and
// TenantByStripeLookup, so it's used directly instead of a new mock.
// ============================================================

// newWebhookTestService is intentionally separate from the newTestService
// helper in cancellation_test.go (different signature/purpose) — kept
// local to this file to avoid touching an existing, unrelated helper.
func newWebhookTestService(t *testing.T, ts tenant.TenantStore, ps *fakeProjectStore) *Service {
	t.Helper()
	return &Service{
		tenantStore:  ts,
		projectStore: ps,
		plans:        NewRegistry("price_pro_test", "price_business_test"),
	}
}

// ============================================================
// customer.subscription.updated
// ============================================================

func TestHandleSubscriptionUpdated_UpgradesTenantPlan(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	if err := ts.Put(&tenant.Tenant{
		ID:                "jekosurabaya",
		StripeCustomerID:  "cus_real123",
		PlanTier:          "free",
		MonthlyEventLimit: 5_000,
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	ps := &fakeProjectStore{}
	svc := newWebhookTestService(t, ts, ps)

	sub := subscriptionPayload{
		ID:       "sub_real123",
		Status:   "active",
		Metadata: map[string]string{"tenant_id": "jekosurabaya"},
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
		t.Fatalf("handleSubscriptionUpdated returned error: %v", err)
	}

	got, err := ts.Get("jekosurabaya")
	if err != nil {
		t.Fatalf("tenant lookup after update: %v", err)
	}

	if got.PlanTier != string(PlanPro) {
		t.Fatalf("expected plan tier %q, got %q", PlanPro, got.PlanTier)
	}
	if got.SubscriptionStatus != "active" {
		t.Fatalf("expected subscription status active, got %q", got.SubscriptionStatus)
	}
	if got.StripeSubscriptionID != "sub_real123" {
		t.Fatalf("expected subscription ID sub_real123, got %q", got.StripeSubscriptionID)
	}
	if got.MonthlyEventLimit != 250_000 {
		t.Fatalf("expected pro monthly event limit 250000, got %d", got.MonthlyEventLimit)
	}

	call, ok := ps.lastCall()
	if !ok {
		t.Fatal("expected ApplyPlanToTenant to be called")
	}
	if call.tenantID != "jekosurabaya" || call.monthlyEventLimit != 250_000 || call.retentionDays != 180 {
		t.Fatalf("unexpected ApplyPlanToTenant call: %+v", call)
	}
}

func TestHandleSubscriptionUpdated_ActiveStatusClearsPriorFailureState(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	pastFailure := time.Now().Add(-time.Hour)
	pastDue := time.Now().Add(-2 * time.Hour)
	deadline := time.Now().Add(2 * time.Hour)
	if err := ts.Put(&tenant.Tenant{
		ID:                   "jekosurabaya",
		StripeCustomerID:     "cus_real123",
		PlanTier:             "pro",
		SubscriptionStatus:   "past_due",
		PaymentFailureCount:  2,
		LastPaymentFailureAt: &pastFailure,
		PaymentFailureReason: "card_declined",
		PastDueSince:         &pastDue,
		GracePeriodDeadline:  &deadline,
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	ps := &fakeProjectStore{}
	svc := newWebhookTestService(t, ts, ps)

	sub := subscriptionPayload{
		ID:       "sub_real123",
		Status:   "active",
		Metadata: map[string]string{"tenant_id": "jekosurabaya"},
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
		t.Fatalf("handleSubscriptionUpdated returned error: %v", err)
	}

	got, err := ts.Get("jekosurabaya")
	if err != nil {
		t.Fatalf("tenant lookup after update: %v", err)
	}

	if got.PaymentFailureCount != 0 {
		t.Fatalf("expected payment failure count reset to 0, got %d", got.PaymentFailureCount)
	}
	if got.LastPaymentFailureAt != nil {
		t.Fatal("expected LastPaymentFailureAt cleared")
	}
	if got.PaymentFailureReason != "" {
		t.Fatalf("expected PaymentFailureReason cleared, got %q", got.PaymentFailureReason)
	}
	if got.PastDueSince != nil {
		t.Fatal("expected PastDueSince cleared")
	}
	if got.GracePeriodDeadline != nil {
		t.Fatal("expected GracePeriodDeadline cleared")
	}
}

func TestHandleSubscriptionUpdated_MissingTenantIDFailsClosed(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	ps := &fakeProjectStore{}
	svc := newWebhookTestService(t, ts, ps)

	sub := subscriptionPayload{ID: "sub_x", Status: "active"}
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
	if ps.callCount() != 0 {
		t.Fatal("ApplyPlanToTenant must not be called when tenant_id is missing")
	}
}

func TestHandleSubscriptionUpdated_UnknownPriceIDFailsClosed(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	if err := ts.Put(&tenant.Tenant{ID: "jekosurabaya", StripeCustomerID: "cus_real123"}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	ps := &fakeProjectStore{}
	svc := newWebhookTestService(t, ts, ps)

	sub := subscriptionPayload{
		ID:       "sub_real123",
		Status:   "active",
		Metadata: map[string]string{"tenant_id": "jekosurabaya"},
	}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{
		{Price: struct {
			ID string `json:"id"`
		}{ID: "price_attacker_controlled"}},
	}

	if err := svc.handleSubscriptionUpdated(context.Background(), sub); err == nil {
		t.Fatal("expected error for unknown Stripe price ID")
	}

	got, _ := ts.Get("jekosurabaya")
	if got.PlanTier != "" {
		t.Fatalf("tenant plan must not change on unknown price ID, got %q", got.PlanTier)
	}
	if ps.callCount() != 0 {
		t.Fatal("ApplyPlanToTenant must not be called when price ID is unknown")
	}
}

// ============================================================
// invoice.payment_failed
// ============================================================

func TestHandleInvoicePaymentFailed_SetsGracePeriod(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	if err := ts.Put(&tenant.Tenant{
		ID:               "jekosurabaya",
		StripeCustomerID: "cus_real123",
		PlanTier:         "pro",
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	svc := newWebhookTestService(t, ts, &fakeProjectStore{})

	inv := invoicePayload{
		ID:           "in_real123",
		Customer:     "cus_real123",
		AttemptCount: 1,
		LastFinalizationError: &struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: "card_declined", Message: "Your card was declined."},
	}

	before := time.Now()
	if err := svc.handleInvoicePaymentFailed(context.Background(), inv); err != nil {
		t.Fatalf("handleInvoicePaymentFailed returned error: %v", err)
	}

	got, err := ts.Get("jekosurabaya")
	if err != nil {
		t.Fatalf("tenant lookup after failure: %v", err)
	}

	if got.SubscriptionStatus != "past_due" {
		t.Fatalf("expected subscription status past_due, got %q", got.SubscriptionStatus)
	}
	if got.PaymentFailureCount != 1 {
		t.Fatalf("expected payment failure count 1, got %d", got.PaymentFailureCount)
	}
	if got.PaymentFailureReason != "card_declined" {
		t.Fatalf("expected reason card_declined, got %q", got.PaymentFailureReason)
	}
	if got.LastPaymentFailureAt == nil || got.LastPaymentFailureAt.Before(before) {
		t.Fatal("expected LastPaymentFailureAt to be set to roughly now")
	}
	if got.PastDueSince == nil {
		t.Fatal("expected PastDueSince to be set")
	}
	if got.GracePeriodDeadline == nil {
		t.Fatal("expected GracePeriodDeadline to be set")
	}
	wantDeadline := got.PastDueSince.Add(invoiceFailureGracePeriod)
	if !got.GracePeriodDeadline.Equal(wantDeadline) {
		t.Fatalf("expected grace deadline %v, got %v", wantDeadline, *got.GracePeriodDeadline)
	}
}

func TestHandleInvoicePaymentFailed_SecondFailureKeepsOriginalDeadline(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	if err := ts.Put(&tenant.Tenant{
		ID:               "jekosurabaya",
		StripeCustomerID: "cus_real123",
		PlanTier:         "pro",
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	svc := newWebhookTestService(t, ts, &fakeProjectStore{})

	inv := invoicePayload{ID: "in_1", Customer: "cus_real123"}
	if err := svc.handleInvoicePaymentFailed(context.Background(), inv); err != nil {
		t.Fatalf("first failure: %v", err)
	}
	first, _ := ts.Get("jekosurabaya")
	firstDeadline := *first.GracePeriodDeadline

	time.Sleep(2 * time.Millisecond) // ensure a distinguishable timestamp if the deadline were wrongly reset

	inv2 := invoicePayload{ID: "in_2", Customer: "cus_real123"}
	if err := svc.handleInvoicePaymentFailed(context.Background(), inv2); err != nil {
		t.Fatalf("second failure: %v", err)
	}
	second, _ := ts.Get("jekosurabaya")

	if second.PaymentFailureCount != 2 {
		t.Fatalf("expected payment failure count 2, got %d", second.PaymentFailureCount)
	}
	if !second.GracePeriodDeadline.Equal(firstDeadline) {
		t.Fatalf("grace period deadline must not reset on repeated failures: first=%v second=%v", firstDeadline, *second.GracePeriodDeadline)
	}
}

func TestHandleInvoicePaymentFailed_UnknownCustomerFailsClosed(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	if err := ts.Put(&tenant.Tenant{
		ID:               "jekosurabaya",
		StripeCustomerID: "cus_real123",
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	svc := newWebhookTestService(t, ts, &fakeProjectStore{})

	// Simulates exactly what `stripe trigger invoice.payment_failed`
	// produces: a freshly generated customer ID that no tenant owns.
	inv := invoicePayload{ID: "in_fixture", Customer: "cus_fixture_generated"}

	if err := svc.handleInvoicePaymentFailed(context.Background(), inv); err == nil {
		t.Fatal("expected error for unknown Stripe customer ID")
	}

	got, _ := ts.Get("jekosurabaya")
	if got.SubscriptionStatus == "past_due" {
		t.Fatal("unrelated tenant must not be marked past_due by an unknown customer's failed invoice")
	}
}

func TestHandleInvoicePaymentFailed_MissingCustomerFailsClosed(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	svc := newWebhookTestService(t, ts, &fakeProjectStore{})

	inv := invoicePayload{ID: "in_x", Customer: ""}
	if err := svc.handleInvoicePaymentFailed(context.Background(), inv); err == nil {
		t.Fatal("expected error when invoice has no customer id")
	}
}
