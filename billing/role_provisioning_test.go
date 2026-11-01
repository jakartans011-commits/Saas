package billing

import (
	"testing"

	"trailseal/rbac"
	"trailseal/tenant"
)

type fakeRoleStore struct {
	putRoles []*rbac.Role
}

func (f *fakeRoleStore) PutRole(role *rbac.Role) error {
	f.putRoles = append(f.putRoles, role)
	return nil
}

func (f *fakeRoleStore) hasRole(roleID string) bool {
	for _, r := range f.putRoles {
		if r.ID == roleID {
			return true
		}
	}
	return false
}

func TestSubscriptionUpdated_ProTier_ProvisionsAdminAndMemberRoles(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", PlanTier: "free"})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())

	roleStore := &fakeRoleStore{}
	svc.SetRoleStore(roleStore)

	sub := subscriptionPayload{
		ID:       "sub_123",
		Status:   "active",
		Metadata: map[string]string{"tenant_id": "acme-corp"},
	}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{{Price: struct {
		ID string `json:"id"`
	}{ID: "price_pro_test"}}}

	if err := svc.handleSubscriptionUpdated(nil, sub); err != nil {
		t.Fatalf("handleSubscriptionUpdated failed: %v", err)
	}

	if !roleStore.hasRole("admin") {
		t.Error("expected admin role to be provisioned for Pro tier, but it wasn't")
	}
	if !roleStore.hasRole("member") {
		t.Error("expected member role to be provisioned for Pro tier, but it wasn't")
	}
}

func TestSubscriptionUpdated_RoleStoreNotSet_DoesNotPanic(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", PlanTier: "free"})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, newFakeWebhookEventStore())
	// SetRoleStore intentionally NOT called — must stay nil-safe.

	sub := subscriptionPayload{
		ID:       "sub_123",
		Status:   "active",
		Metadata: map[string]string{"tenant_id": "acme-corp"},
	}
	sub.Items.Data = []struct {
		Price struct {
			ID string `json:"id"`
		} `json:"price"`
	}{{Price: struct {
		ID string `json:"id"`
	}{ID: "price_pro_test"}}}

	if err := svc.handleSubscriptionUpdated(nil, sub); err != nil {
		t.Fatalf("expected no error when roleStore is unset, got: %v", err)
	}
}
