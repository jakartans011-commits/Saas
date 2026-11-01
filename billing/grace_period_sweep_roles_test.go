package billing

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"trailseal/rbac"
	"trailseal/tenant"
)

type sweepRoleRecorder struct {
	mu      sync.Mutex
	deleted []string
}

func (r *sweepRoleRecorder) PutRole(*rbac.Role) error { return nil }

func (r *sweepRoleRecorder) DeleteRole(tenantID, roleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, tenantID+"|"+roleID)
	return nil
}

func (r *sweepRoleRecorder) sorted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.deleted...)
	sort.Strings(out)
	return out
}

// Gagal bayar lewat grace period harus menangguhkan admin/member (role dicabut)
// untuk tenant yang kedaluwarsa SAJA.
func TestSweepExpiredGracePeriods_RevokesPaidRoles_ExpiredOnly(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	projectStore := &fakeProjectStore{}
	plans := NewRegistry("price_pro_test", "price_business_test")
	svc := NewService(tenantStore, projectStore, nil, plans, "whsec_test", "", "")
	roles := &sweepRoleRecorder{}
	svc.SetRoleStore(roles)

	seedPastDueTenant(t, tenantStore, "tenant-expired", time.Now().Add(-1*time.Hour))
	seedPastDueTenant(t, tenantStore, "tenant-still-in-grace", time.Now().Add(48*time.Hour))

	if _, err := svc.SweepExpiredGracePeriods(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"tenant-expired|admin", "tenant-expired|member"}
	if got := roles.sorted(); !reflect.DeepEqual(got, want) {
		t.Errorf("revoked roles = %v, want %v", got, want)
	}
}

// Pencabutan role tidak boleh bergantung pada keberhasilan ApplyPlanToTenant:
// setelah tenant tersimpan sebagai Free, ia tidak muncul lagi di sweep berikutnya.
func TestSweepExpiredGracePeriods_RevokesPaidRoles_EvenWhenProjectStoreFails(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	projectStore := &callTrackingFailingStore{err: errors.New("simulated project store failure")}
	plans := NewRegistry("price_pro_test", "price_business_test")
	svc := NewService(tenantStore, projectStore, nil, plans, "whsec_test", "", "")
	roles := &sweepRoleRecorder{}
	svc.SetRoleStore(roles)

	seedPastDueTenant(t, tenantStore, "tenant-expired", time.Now().Add(-1*time.Hour))

	if _, err := svc.SweepExpiredGracePeriods(context.Background()); err == nil {
		t.Fatal("expected error from failing project store")
	}

	want := []string{"tenant-expired|admin", "tenant-expired|member"}
	if got := roles.sorted(); !reflect.DeepEqual(got, want) {
		t.Errorf("revoked roles = %v, want %v", got, want)
	}
}