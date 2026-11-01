package billing

import (
	"context"
	"errors"
	"testing"

	"trailseal/rbac"
	"trailseal/tenant"
)

type fakeRoleDeleterStore struct {
	deleted []string
	failOn  string
}

func (f *fakeRoleDeleterStore) PutRole(role *rbac.Role) error { return nil }

func (f *fakeRoleDeleterStore) DeleteRole(tenantID, roleID string) error {
	f.deleted = append(f.deleted, tenantID+":"+roleID)
	if roleID == f.failOn {
		return errors.New("boom")
	}
	return nil
}

func downgradeSub() subscriptionPayload {
	return subscriptionPayload{ID: "sub_1", Metadata: map[string]string{"tenant_id": "acme-corp"}}
}

func TestSubscriptionDeleted_RevokesAdminAndMember(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	seedTenant(t, ts, &tenant.Tenant{ID: "acme-corp", PlanTier: "pro"})
	svc := newTestService(t, ts, &fakeProjectStore{}, newFakeWebhookEventStore())
	rs := &fakeRoleDeleterStore{}
	svc.SetRoleStore(rs)

	if err := svc.handleSubscriptionDeleted(context.Background(), downgradeSub()); err != nil {
		t.Fatalf("downgrade gagal: %v", err)
	}
	want := []string{"acme-corp:admin", "acme-corp:member"}
	if len(rs.deleted) != 2 || rs.deleted[0] != want[0] || rs.deleted[1] != want[1] {
		t.Errorf("deleted = %v, want %v", rs.deleted, want)
	}
}

func TestSubscriptionDeleted_RevokeFailureDoesNotBlockDowngrade(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	seedTenant(t, ts, &tenant.Tenant{ID: "acme-corp", PlanTier: "pro"})
	svc := newTestService(t, ts, &fakeProjectStore{}, newFakeWebhookEventStore())
	rs := &fakeRoleDeleterStore{failOn: "admin"}
	svc.SetRoleStore(rs)

	if err := svc.handleSubscriptionDeleted(context.Background(), downgradeSub()); err != nil {
		t.Fatalf("downgrade harus tetap sukses: %v", err)
	}
	if len(rs.deleted) != 2 {
		t.Errorf("member harus tetap dicoba setelah admin gagal, deleted = %v", rs.deleted)
	}
	got, _ := ts.Get("acme-corp")
	if got.PlanTier != "free" {
		t.Errorf("PlanTier = %q, want free", got.PlanTier)
	}
}

// Store tanpa DeleteRole (mis. fakeRoleStore) tidak boleh membuat panic/error.
func TestSubscriptionDeleted_StoreWithoutDeleteRole(t *testing.T) {
	ts := tenant.NewInMemoryTenantStore()
	seedTenant(t, ts, &tenant.Tenant{ID: "acme-corp", PlanTier: "pro"})
	svc := newTestService(t, ts, &fakeProjectStore{}, newFakeWebhookEventStore())
	svc.SetRoleStore(&fakeRoleStore{})

	if err := svc.handleSubscriptionDeleted(context.Background(), downgradeSub()); err != nil {
		t.Fatalf("downgrade gagal: %v", err)
	}
}
