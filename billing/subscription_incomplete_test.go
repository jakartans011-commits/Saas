package billing

import (
	"context"
	"testing"

	"trailseal/tenant"
)

func TestHandleSubscriptionUpdated_IncompleteDoesNotGrantPaidPlan(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                   "acme-corp",
		StripeSubscriptionID: "sub_123",
		PlanTier:             "free",
	})
	projectStore := &fakeProjectStore{}
	svc := newTestService(t, tenantStore, projectStore, newFakeWebhookEventStore())

	sub := subscriptionPayload{
		ID:       "sub_123",
		Status:   "incomplete",
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
		t.Fatal(err)
	}
	if got.PlanTier == string(PlanPro) {
		t.Errorf("subscription incomplete tidak boleh memberi plan pro (PlanTier=%q)", got.PlanTier)
	}
	if call, called := projectStore.lastCall(); called {
		pro, _ := NewRegistry("price_pro_test", "price_business_test").Get(PlanPro)
		if call.monthlyEventLimit == pro.MonthlyEventLimit {
			t.Error("limit events Pro tidak boleh diterapkan untuk subscription incomplete")
		}
	}
}