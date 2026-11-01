package billing

import (
	"context"
	"testing"
	"time"

	"trailseal/tenant"
)

// Downgrade gagal-bayar harus mengembalikan batas event record tenant ke Free
// (sama dengan jalur subscription.deleted), supaya dashboard usage tidak
// menampilkan kuota Pro/Business.
func TestSweepExpiredGracePeriods_ResetsTenantEventLimitToFree(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	projectStore := &fakeProjectStore{}
	plans := NewRegistry("price_pro_test", "price_business_test")
	svc := NewService(tenantStore, projectStore, nil, plans, "whsec_test", "", "")

	seedPastDueTenant(t, tenantStore, "tenant-expired", time.Now().Add(-1*time.Hour))

	tn, err := tenantStore.Get("tenant-expired")
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	pro, _ := plans.Get(PlanPro)
	tn.MonthlyEventLimit = pro.MonthlyEventLimit
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("put tenant: %v", err)
	}

	if _, err := svc.SweepExpiredGracePeriods(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := tenantStore.Get("tenant-expired")
	if err != nil {
		t.Fatalf("get tenant after sweep: %v", err)
	}
	if want := plans.Free().MonthlyEventLimit; got.MonthlyEventLimit != want {
		t.Errorf("MonthlyEventLimit = %d, want Free limit %d", got.MonthlyEventLimit, want)
	}
}
