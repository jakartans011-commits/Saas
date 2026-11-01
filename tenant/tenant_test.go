// tenant/tenant_test.go
package tenant

import (
	"testing"
	"time"
)

func TestInMemoryTenantStore_ListExpiredGracePeriods(t *testing.T) {
	store := NewInMemoryTenantStore()

	now := time.Now()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)
	deletedAt := now.Add(-1 * time.Hour)

	seed := []*Tenant{
		{
			// HARUS masuk hasil: past_due + deadline sudah lewat
			ID:                  "tenant-expired",
			Name:                "Expired Grace",
			Status:              TenantStatusActive,
			CreatedAt:           now,
			SubscriptionStatus:  "past_due",
			GracePeriodDeadline: &past,
		},
		{
			// TIDAK boleh masuk: deadline belum lewat
			ID:                  "tenant-not-yet-expired",
			Name:                "Still In Grace",
			Status:              TenantStatusActive,
			CreatedAt:           now,
			SubscriptionStatus:  "past_due",
			GracePeriodDeadline: &future,
		},
		{
			// TIDAK boleh masuk: status bukan past_due
			ID:                  "tenant-active",
			Name:                "Fully Active",
			Status:              TenantStatusActive,
			CreatedAt:           now,
			SubscriptionStatus:  "active",
			GracePeriodDeadline: &past,
		},
		{
			// TIDAK boleh masuk: sudah soft-deleted
			ID:                  "tenant-deleted-past-due",
			Name:                "Deleted But Past Due",
			Status:              TenantStatusDeprovisioned,
			CreatedAt:           now,
			DeletedAt:           &deletedAt,
			SubscriptionStatus:  "past_due",
			GracePeriodDeadline: &past,
		},
		{
			// TIDAK boleh masuk: tidak ada deadline sama sekali
			ID:                  "tenant-no-deadline",
			Name:                "No Deadline Set",
			Status:              TenantStatusActive,
			CreatedAt:           now,
			SubscriptionStatus:  "past_due",
			GracePeriodDeadline: nil,
		},
	}

	for _, tn := range seed {
		if err := store.Put(tn); err != nil {
			t.Fatalf("Put failed for %s: %v", tn.ID, err)
		}
	}

	result, err := store.ListExpiredGracePeriods(now)
	if err != nil {
		t.Fatalf("ListExpiredGracePeriods failed: %v", err)
	}

	if len(result) != 1 {
		ids := make([]string, 0, len(result))
		for _, r := range result {
			ids = append(ids, r.ID)
		}
		t.Fatalf("expected exactly 1 expired tenant, got %d: %v", len(result), ids)
	}

	if result[0].ID != "tenant-expired" {
		t.Fatalf("expected tenant-expired, got %s", result[0].ID)
	}

	t.Log("OK: only the correctly expired, non-deleted, past_due tenant is returned")
}

func TestInMemoryTenantStore_FindByStripeCustomerID(t *testing.T) {
	store := NewInMemoryTenantStore()
	now := time.Now()

	tenants := []*Tenant{
		{
			ID:                   "tenant-stripe-a",
			Name:                 "Stripe Tenant A",
			Status:               TenantStatusActive,
			CreatedAt:            now,
			StripeCustomerID:     "cus_test_123",
			StripeSubscriptionID: "sub_test_123",
			PlanTier:             "pro",
			SubscriptionStatus:   "active",
		},
		{
			ID:                   "tenant-stripe-b",
			Name:                 "Stripe Tenant B",
			Status:               TenantStatusActive,
			CreatedAt:            now,
			StripeCustomerID:     "cus_test_456",
			StripeSubscriptionID: "sub_test_456",
			PlanTier:             "business",
			SubscriptionStatus:   "active",
		},
	}

	for _, tn := range tenants {
		if err := store.Put(tn); err != nil {
			t.Fatalf("Put failed for tenant %s: %v", tn.ID, err)
		}
	}

	t.Run("find_existing_customer", func(t *testing.T) {
		got, err := store.FindByStripeCustomerID("cus_test_123")
		if err != nil {
			t.Fatalf("FindByStripeCustomerID failed: %v", err)
		}
		if got.ID != "tenant-stripe-a" {
			t.Fatalf("expected tenant-stripe-a, got %s", got.ID)
		}
		if got.PlanTier != "pro" {
			t.Fatalf("expected plan tier pro, got %s", got.PlanTier)
		}
	})

	t.Run("find_second_customer", func(t *testing.T) {
		got, err := store.FindByStripeCustomerID("cus_test_456")
		if err != nil {
			t.Fatalf("FindByStripeCustomerID failed: %v", err)
		}
		if got.ID != "tenant-stripe-b" {
			t.Fatalf("expected tenant-stripe-b, got %s", got.ID)
		}
	})

	t.Run("unknown_customer_returns_error", func(t *testing.T) {
		_, err := store.FindByStripeCustomerID("cus_does_not_exist")
		if err == nil {
			t.Fatal("expected error for unknown Stripe customer")
		}
		t.Logf("OK: unknown customer rejected: %v", err)
	})

	t.Run("empty_customer_id_returns_error", func(t *testing.T) {
		_, err := store.FindByStripeCustomerID("")
		if err == nil {
			t.Fatal("expected error for empty Stripe customer ID")
		}
		t.Logf("OK: empty customer ID rejected: %v", err)
	})

	t.Run("soft_deleted_tenant_is_not_returned", func(t *testing.T) {
		deletedAt := time.Now()
		tn := &Tenant{
			ID:                 "tenant-stripe-deleted",
			Name:               "Deleted Stripe Tenant",
			Status:             TenantStatusActive,
			CreatedAt:          now,
			DeletedAt:          &deletedAt,
			StripeCustomerID:   "cus_deleted_123",
			PlanTier:           "pro",
			SubscriptionStatus: "active",
		}
		// Put biasa akan menyimpan meski DeletedAt terisi (soft delete
		// disimulasikan langsung di struct, bukan lewat method delete).
		if err := store.Put(tn); err != nil {
			t.Fatalf("Put deleted tenant failed: %v", err)
		}

		_, err := store.FindByStripeCustomerID("cus_deleted_123")
		if err == nil {
			t.Fatal("soft-deleted tenant must not be returned")
		}
		t.Logf("OK: soft-deleted tenant rejected: %v", err)
	})
}