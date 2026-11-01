package billing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"trailseal/tenant"
)

func seedPastDueTenant(t *testing.T, store *tenant.InMemoryTenantStore, id string, deadline time.Time) {
	t.Helper()
	past := time.Now().Add(-10 * 24 * time.Hour)
	if err := store.Put(&tenant.Tenant{
		ID:                   id,
		Name:                 id,
		Status:               tenant.TenantStatusActive,
		PlanTier:             "pro",
		SubscriptionStatus:   "past_due",
		PaymentFailureCount:  3,
		PaymentFailureReason: "card_declined",
		PastDueSince:         &past,
		GracePeriodDeadline:  &deadline,
	}); err != nil {
		t.Fatalf("failed to seed tenant %s: %v", id, err)
	}
}

func TestSweepExpiredGracePeriods_DowngradesExpiredOnly(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	projectStore := &fakeProjectStore{}
	plans := NewRegistry("price_pro_test", "price_business_test")
	svc := NewService(tenantStore, projectStore, nil, plans, "whsec_test", "", "")

	// Expired: deadline sudah lewat 1 jam.
	seedPastDueTenant(t, tenantStore, "tenant-expired", time.Now().Add(-1*time.Hour))
	// Belum expired: deadline masih 2 hari lagi.
	seedPastDueTenant(t, tenantStore, "tenant-still-in-grace", time.Now().Add(48*time.Hour))

	n, err := svc.SweepExpiredGracePeriods(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 tenant downgraded, got %d", n)
	}

	expired, err := tenantStore.Get("tenant-expired")
	if err != nil {
		t.Fatalf("failed to get tenant-expired: %v", err)
	}
	if expired.PlanTier != "free" {
		t.Errorf("expected tenant-expired downgraded to free, got %s", expired.PlanTier)
	}
	if expired.SubscriptionStatus != "canceled_nonpayment" {
		t.Errorf("expected status canceled_nonpayment, got %s", expired.SubscriptionStatus)
	}
	if expired.GracePeriodDeadline != nil {
		t.Error("expected GracePeriodDeadline cleared after downgrade")
	}
	if expired.PaymentFailureCount != 0 {
		t.Errorf("expected PaymentFailureCount reset to 0, got %d", expired.PaymentFailureCount)
	}

	stillInGrace, err := tenantStore.Get("tenant-still-in-grace")
	if err != nil {
		t.Fatalf("failed to get tenant-still-in-grace: %v", err)
	}
	if stillInGrace.PlanTier != "pro" {
		t.Errorf("expected tenant-still-in-grace untouched (still pro), got %s", stillInGrace.PlanTier)
	}
	if stillInGrace.SubscriptionStatus != "past_due" {
		t.Errorf("expected tenant-still-in-grace still past_due, got %s", stillInGrace.SubscriptionStatus)
	}

	if projectStore.callCount() != 1 {
		t.Errorf("expected exactly 1 ApplyPlanToTenant call (for the expired tenant only), got %d", projectStore.callCount())
	}
}

func TestSweepExpiredGracePeriods_NoExpiredTenants_NoOp(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	projectStore := &fakeProjectStore{}
	plans := NewRegistry("price_pro_test", "price_business_test")
	svc := NewService(tenantStore, projectStore, nil, plans, "whsec_test", "", "")

	seedPastDueTenant(t, tenantStore, "tenant-safe", time.Now().Add(72*time.Hour))

	n, err := svc.SweepExpiredGracePeriods(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 downgrades, got %d", n)
	}
	if projectStore.callCount() != 0 {
		t.Errorf("expected 0 ApplyPlanToTenant calls, got %d", projectStore.callCount())
	}
}

// callTrackingFailingStore mencatat SETIAP percobaan panggilan terlepas
// dari hasilnya (berbeda dari fakeProjectStore, yang return error lebih
// dulu SEBELUM sempat mencatat call-nya) — dibutuhkan supaya test di
// bawah bisa membuktikan sweep benar-benar MENCOBA semua tenant, bukan
// berhenti di percobaan pertama yang gagal.
type callTrackingFailingStore struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *callTrackingFailingStore) ApplyPlanToTenant(_ context.Context, tenantID string, _ int64, _ int) error {
	f.mu.Lock()
	f.calls = append(f.calls, tenantID)
	f.mu.Unlock()
	return f.err
}

func (f *callTrackingFailingStore) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// TestSweepExpiredGracePeriods_OneFailureDoesNotBlockOthers proves the
// "satu tenant bermasalah tidak boleh menghentikan tenant lain" guarantee:
// if ApplyPlanToTenant fails for one tenant, the sweep must still attempt
// the remaining tenants, returning 0 successes and the first error when
// every call fails.
func TestSweepExpiredGracePeriods_OneFailureDoesNotBlockOthers(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	projectStore := &callTrackingFailingStore{err: errors.New("simulated project store failure")}
	plans := NewRegistry("price_pro_test", "price_business_test")
	svc := NewService(tenantStore, projectStore, nil, plans, "whsec_test", "", "")

	seedPastDueTenant(t, tenantStore, "tenant-a", time.Now().Add(-1*time.Hour))
	seedPastDueTenant(t, tenantStore, "tenant-b", time.Now().Add(-1*time.Hour))

	n, err := svc.SweepExpiredGracePeriods(context.Background())
	if err == nil {
		t.Fatal("expected error to be returned when ApplyPlanToTenant fails for all tenants")
	}
	if n != 0 {
		t.Errorf("expected 0 successful downgrades when every ApplyPlanToTenant call fails, got %d", n)
	}
	if projectStore.callCount() != 2 {
		t.Errorf("expected sweep to attempt BOTH tenants despite failures, got %d calls", projectStore.callCount())
	}
}

func TestSweepExpiredGracePeriods_StoreWithoutListerInterface_ReturnsError(t *testing.T) {
	// A minimal TenantStore that does NOT implement TenantPastDueLister,
	// proving the type-assertion guard actually fails closed rather than
	// panicking or silently sweeping nothing.
	svc := NewService(&noListerTenantStore{}, &fakeProjectStore{}, nil,
		NewRegistry("price_pro_test", "price_business_test"), "whsec_test", "", "")

	_, err := svc.SweepExpiredGracePeriods(context.Background())
	if err == nil {
		t.Fatal("expected error when tenant store does not support past-due listing")
	}
}

type noListerTenantStore struct{}

func (noListerTenantStore) Get(tenantID string) (*tenant.Tenant, error) {
	return nil, errors.New("not_implemented")
}
func (noListerTenantStore) Put(t *tenant.Tenant) error { return nil }