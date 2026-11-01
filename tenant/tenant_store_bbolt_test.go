// tenant/tenant_store_bbolt_test.go
package tenant

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestTenantBoltDB(t *testing.T, path string) *bolt.DB {
	t.Helper()

	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test bolt db: %v", err)
	}

	return db
}

func TestBoltTenantStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tenants_test.db")

	t.Run("put_and_get_roundtrip", func(t *testing.T) {
		db := openTestTenantBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltTenantStore(db)
		if err != nil {
			t.Fatalf("NewBoltTenantStore failed: %v", err)
		}

		tn := &Tenant{
			ID:        "tenant-a",
			Name:      "Tenant A",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}

		if err := store.Put(tn); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		got, err := store.Get("tenant-a")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if got.Name != "Tenant A" || got.Status != TenantStatusActive {
			t.Fatalf("roundtrip mismatch: got %+v", got)
		}

		t.Log("OK: put/get roundtrip matches")
	})

	t.Run("get_nonexistent_returns_error", func(t *testing.T) {
		db := openTestTenantBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltTenantStore(db)
		if err != nil {
			t.Fatalf("NewBoltTenantStore failed: %v", err)
		}

		_, err = store.Get("does-not-exist")
		if err == nil {
			t.Fatal("expected error for nonexistent tenant, got nil")
		}

		t.Logf("OK: got expected error: %v", err)
	})

	t.Run("status_transition_persists", func(t *testing.T) {
		db := openTestTenantBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltTenantStore(db)
		if err != nil {
			t.Fatalf("NewBoltTenantStore failed: %v", err)
		}

		tn := &Tenant{
			ID:        "tenant-b",
			Name:      "Tenant B",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}

		if err := store.Put(tn); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// Simulasikan deprovision: ubah status, Put ulang
		// (pola sama persis dengan DeprovisionTenant di provisioning.go).
		tn.Status = TenantStatusDeprovisioned

		if err := store.Put(tn); err != nil {
			t.Fatalf("Put (status update) failed: %v", err)
		}

		got, err := store.Get("tenant-b")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if got.Status != TenantStatusDeprovisioned {
			t.Fatalf(
				"expected status deprovisioned, got %s",
				got.Status,
			)
		}

		t.Log("OK: status transition persisted correctly")
	})

	t.Run("data_survives_close_and_reopen", func(t *testing.T) {
		persistPath := filepath.Join(
			t.TempDir(),
			"tenants_persist_test.db",
		)

		db1 := openTestTenantBoltDB(t, persistPath)

		store1, err := NewBoltTenantStore(db1)
		if err != nil {
			t.Fatalf("NewBoltTenantStore failed: %v", err)
		}

		if err := store1.Put(&Tenant{
			ID:        "tenant-persist",
			Name:      "Persist Test",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		if err := db1.Close(); err != nil {
			t.Fatalf("failed to close db: %v", err)
		}

		db2 := openTestTenantBoltDB(t, persistPath)
		defer db2.Close()

		store2, err := NewBoltTenantStore(db2)
		if err != nil {
			t.Fatalf("NewBoltTenantStore (reopen) failed: %v", err)
		}

		got, err := store2.Get("tenant-persist")
		if err != nil {
			t.Fatalf(
				"PERSISTENCE FAILED: tenant not found after reopen: %v",
				err,
			)
		}

		if got.Name != "Persist Test" {
			t.Fatalf(
				"persisted tenant corrupted: got %+v",
				got,
			)
		}

		t.Log("OK: tenant data survives close+reopen")
	})

	t.Run("billing_fields_persist", func(t *testing.T) {
		billingPath := filepath.Join(
			t.TempDir(),
			"tenants_billing_test.db",
		)

		db := openTestTenantBoltDB(t, billingPath)
		defer db.Close()

		store, err := NewBoltTenantStore(db)
		if err != nil {
			t.Fatalf("NewBoltTenantStore failed: %v", err)
		}

		tn := &Tenant{
			ID:        "tenant-billing",
			Name:      "Billing Tenant",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),

			StripeCustomerID:     "cus_test_123",
			StripeSubscriptionID: "sub_test_123",
			PlanTier:             "pro",
			SubscriptionStatus:   "active",
		}

		if err := store.Put(tn); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		got, err := store.Get("tenant-billing")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if got.StripeCustomerID != "cus_test_123" {
			t.Fatalf(
				"StripeCustomerID mismatch: want %q, got %q",
				"cus_test_123",
				got.StripeCustomerID,
			)
		}

		if got.StripeSubscriptionID != "sub_test_123" {
			t.Fatalf(
				"StripeSubscriptionID mismatch: want %q, got %q",
				"sub_test_123",
				got.StripeSubscriptionID,
			)
		}

		if got.PlanTier != "pro" {
			t.Fatalf(
				"PlanTier mismatch: want %q, got %q",
				"pro",
				got.PlanTier,
			)
		}

		if got.SubscriptionStatus != "active" {
			t.Fatalf(
				"SubscriptionStatus mismatch: want %q, got %q",
				"active",
				got.SubscriptionStatus,
			)
		}

		t.Log("OK: billing fields persist correctly")
	})
}

func TestBoltTenantStore_FindByStripeCustomerID(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"stripe_customer_lookup_test.db",
	)

	db := openTestTenantBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}

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
			t.Fatalf(
				"Put failed for tenant %s: %v",
				tn.ID,
				err,
			)
		}
	}

	t.Run("find_existing_customer", func(t *testing.T) {
		got, err := store.FindByStripeCustomerID("cus_test_123")
		if err != nil {
			t.Fatalf(
				"FindByStripeCustomerID failed: %v",
				err,
			)
		}

		if got.ID != "tenant-stripe-a" {
			t.Fatalf(
				"expected tenant-stripe-a, got %s",
				got.ID,
			)
		}

		if got.StripeCustomerID != "cus_test_123" {
			t.Fatalf(
				"expected Stripe customer cus_test_123, got %s",
				got.StripeCustomerID,
			)
		}

		if got.PlanTier != "pro" {
			t.Fatalf(
				"expected plan tier pro, got %s",
				got.PlanTier,
			)
		}
	})

	t.Run("find_second_customer", func(t *testing.T) {
		got, err := store.FindByStripeCustomerID("cus_test_456")
		if err != nil {
			t.Fatalf(
				"FindByStripeCustomerID failed: %v",
				err,
			)
		}

		if got.ID != "tenant-stripe-b" {
			t.Fatalf(
				"expected tenant-stripe-b, got %s",
				got.ID,
			)
		}

		if got.StripeCustomerID != "cus_test_456" {
			t.Fatalf(
				"expected Stripe customer cus_test_456, got %s",
				got.StripeCustomerID,
			)
		}

		if got.PlanTier != "business" {
			t.Fatalf(
				"expected plan tier business, got %s",
				got.PlanTier,
			)
		}
	})

	t.Run("unknown_customer_returns_error", func(t *testing.T) {
		_, err := store.FindByStripeCustomerID(
			"cus_does_not_exist",
		)

		if err == nil {
			t.Fatal(
				"expected error for unknown Stripe customer",
			)
		}

		t.Logf(
			"OK: unknown customer rejected: %v",
			err,
		)
	})

	t.Run("empty_customer_id_returns_error", func(t *testing.T) {
		_, err := store.FindByStripeCustomerID("")

		if err == nil {
			t.Fatal(
				"expected error for empty Stripe customer ID",
			)
		}

		t.Logf(
			"OK: empty customer ID rejected: %v",
			err,
		)
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

		if err := store.Put(tn); err != nil {
			t.Fatalf(
				"Put deleted tenant failed: %v",
				err,
			)
		}

		_, err := store.FindByStripeCustomerID(
			"cus_deleted_123",
		)

		if err == nil {
			t.Fatal(
				"soft-deleted tenant must not be returned",
			)
		}

		t.Logf(
			"OK: soft-deleted tenant rejected: %v",
			err,
		)
	})
}

func TestBoltTenantStore_PutIfNotExists(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "put_if_not_exists_test.db")

	db := openTestTenantBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}

	t.Run("creates_new_tenant_successfully", func(t *testing.T) {
		tn := &Tenant{
			ID:        "tenant-new",
			Name:      "New Tenant",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}

		if err := store.PutIfNotExists(tn); err != nil {
			t.Fatalf("PutIfNotExists failed for new tenant: %v", err)
		}

		got, err := store.Get("tenant-new")
		if err != nil {
			t.Fatalf("Get after PutIfNotExists failed: %v", err)
		}
		if got.Name != "New Tenant" {
			t.Fatalf("expected Name 'New Tenant', got %q", got.Name)
		}
	})

	t.Run("rejects_when_active_tenant_already_exists", func(t *testing.T) {
		tn := &Tenant{
			ID:        "tenant-dup",
			Name:      "Original",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}
		if err := store.PutIfNotExists(tn); err != nil {
			t.Fatalf("first PutIfNotExists failed: %v", err)
		}

		dup := &Tenant{
			ID:        "tenant-dup",
			Name:      "Attempted Duplicate",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}
		err := store.PutIfNotExists(dup)
		if err != ErrTenantAlreadyExists {
			t.Fatalf("expected ErrTenantAlreadyExists, got %v", err)
		}

		// pastikan data lama tidak tertimpa
		got, err := store.Get("tenant-dup")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Name != "Original" {
			t.Fatalf("existing tenant was overwritten: got Name %q", got.Name)
		}
	})

	t.Run("allows_recreate_when_existing_is_soft_deleted", func(t *testing.T) {
		deletedAt := time.Now()
		tn := &Tenant{
			ID:        "tenant-recreate",
			Name:      "Old Deleted",
			Status:    TenantStatusDeprovisioned,
			CreatedAt: time.Now(),
			DeletedAt: &deletedAt,
		}
		if err := store.Put(tn); err != nil {
			t.Fatalf("Put (seed soft-deleted) failed: %v", err)
		}

		recreated := &Tenant{
			ID:        "tenant-recreate",
			Name:      "Recreated",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}
		if err := store.PutIfNotExists(recreated); err != nil {
			t.Fatalf("PutIfNotExists should succeed over soft-deleted tenant: %v", err)
		}

		got, err := store.Get("tenant-recreate")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Name != "Recreated" || got.DeletedAt != nil {
			t.Fatalf("expected recreated tenant to be active/undeleted, got %+v", got)
		}
	})

	t.Run("empty_id_returns_error", func(t *testing.T) {
		tn := &Tenant{ID: "", Name: "No ID"}
		if err := store.PutIfNotExists(tn); err == nil {
			t.Fatal("expected error for empty tenant ID, got nil")
		}
	})
}

func TestBoltTenantStore_Delete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "delete_test.db")

	db := openTestTenantBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}

	t.Run("deletes_existing_tenant", func(t *testing.T) {
		tn := &Tenant{
			ID:        "tenant-to-delete",
			Name:      "Delete Me",
			Status:    TenantStatusActive,
			CreatedAt: time.Now(),
		}
		if err := store.Put(tn); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		if err := store.Delete("tenant-to-delete"); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// harus hilang total, termasuk lewat GetIncludingDeleted
		if _, err := store.GetIncludingDeleted("tenant-to-delete"); err == nil {
			t.Fatal("expected tenant to be gone after hard delete, but it still exists")
		}
	})

	t.Run("delete_nonexistent_does_not_error", func(t *testing.T) {
		// bbolt Delete pada key yang tidak ada = no-op, tidak error.
		// Test ini mendokumentasikan behavior itu secara eksplisit.
		if err := store.Delete("tenant-never-existed"); err != nil {
			t.Fatalf("expected no error deleting nonexistent tenant, got: %v", err)
		}
	})
}

func TestBoltTenantStore_ListExpiredGracePeriods(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "grace_periods_test.db")

	db := openTestTenantBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}

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
