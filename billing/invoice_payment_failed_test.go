package billing

import (
	"context"
	"testing"
	"time"

	"trailseal/tenant"
)

func TestHandleInvoicePaymentFailed_FirstFailure_SetsPastDueAndDeadline(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	_ = tenantStore.Put(&tenant.Tenant{
		ID: "tenant-1", Name: "Acme", Status: tenant.TenantStatusActive,
		StripeCustomerID: "cus_123", PlanTier: "pro", SubscriptionStatus: "active",
	})
	svc := NewService(tenantStore, &fakeProjectStore{}, nil,
		NewRegistry("price_pro_test", "price_business_test"), "whsec_test", "", "")

	inv := invoicePayload{ID: "in_1", Customer: "cus_123"}
	before := time.Now()

	if err := svc.handleInvoicePaymentFailed(context.Background(), inv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := tenantStore.Get("tenant-1")
	if err != nil {
		t.Fatalf("failed to get tenant: %v", err)
	}
	if got.SubscriptionStatus != "past_due" {
		t.Errorf("expected status past_due, got %s", got.SubscriptionStatus)
	}
	if got.PaymentFailureCount != 1 {
		t.Errorf("expected PaymentFailureCount 1, got %d", got.PaymentFailureCount)
	}
	if got.PastDueSince == nil || got.PastDueSince.Before(before) {
		t.Error("expected PastDueSince to be set to roughly now")
	}
	if got.GracePeriodDeadline == nil || !got.GracePeriodDeadline.After(*got.PastDueSince) {
		t.Error("expected GracePeriodDeadline to be set after PastDueSince")
	}
}

func TestHandleInvoicePaymentFailed_SecondFailure_DoesNotResetDeadline(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	firstFailure := time.Now().Add(-2 * time.Hour)
	originalDeadline := firstFailure.Add(invoiceFailureGracePeriod)
	_ = tenantStore.Put(&tenant.Tenant{
		ID: "tenant-1", StripeCustomerID: "cus_123", Status: tenant.TenantStatusActive,
		SubscriptionStatus: "past_due", PaymentFailureCount: 1,
		PastDueSince: &firstFailure, GracePeriodDeadline: &originalDeadline,
	})
	svc := NewService(tenantStore, &fakeProjectStore{}, nil,
		NewRegistry("price_pro_test", "price_business_test"), "whsec_test", "", "")

	inv := invoicePayload{ID: "in_2", Customer: "cus_123"}
	if err := svc.handleInvoicePaymentFailed(context.Background(), inv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tenantStore.Get("tenant-1")
	if got.PaymentFailureCount != 2 {
		t.Errorf("expected count incremented to 2, got %d", got.PaymentFailureCount)
	}
	if !got.GracePeriodDeadline.Equal(originalDeadline) {
		t.Errorf("expected GracePeriodDeadline UNCHANGED on retry, got %v (original %v)", got.GracePeriodDeadline, originalDeadline)
	}
}

func TestHandleInvoicePaymentFailed_MissingCustomer_Rejected(t *testing.T) {
	svc := NewService(tenant.NewInMemoryTenantStore(), &fakeProjectStore{}, nil,
		NewRegistry("price_pro_test", "price_business_test"), "whsec_test", "", "")

	err := svc.handleInvoicePaymentFailed(context.Background(), invoicePayload{ID: "in_1"})
	if err == nil {
		t.Fatal("expected error when customer id is missing")
	}
}

func TestHandleInvoicePaymentFailed_UnknownCustomer_Rejected(t *testing.T) {
	svc := NewService(tenant.NewInMemoryTenantStore(), &fakeProjectStore{}, nil,
		NewRegistry("price_pro_test", "price_business_test"), "whsec_test", "", "")

	err := svc.handleInvoicePaymentFailed(context.Background(), invoicePayload{ID: "in_1", Customer: "cus_ghost"})
	if err == nil {
		t.Fatal("expected error for unknown stripe customer")
	}
}

func TestHandleSubscriptionUpdated_ActiveStatus_ClearsPaymentFailureFields(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	past := time.Now().Add(-1 * time.Hour)
	deadline := time.Now().Add(3 * time.Hour)
	_ = tenantStore.Put(&tenant.Tenant{
		ID: "tenant-1", Status: tenant.TenantStatusActive,
		SubscriptionStatus: "past_due", PaymentFailureCount: 2, PaymentFailureReason: "card_declined",
		PastDueSince: &past, GracePeriodDeadline: &deadline,
	})
	svc := NewService(tenantStore, &fakeProjectStore{}, nil,
		NewRegistry("price_pro_test", "price_business_test"), "whsec_test", "", "")

	sub := subscriptionPayload{ID: "sub_1", Status: "active", Metadata: map[string]string{"tenant_id": "tenant-1"}}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{{Price: struct {
		ID string `json:"id"`
	}{ID: "price_pro_test"}}}

	if err := svc.handleSubscriptionUpdated(context.Background(), sub); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tenantStore.Get("tenant-1")
	if got.PaymentFailureCount != 0 || got.PastDueSince != nil || got.GracePeriodDeadline != nil {
		t.Error("expected payment failure fields cleared when subscription becomes active")
	}
}