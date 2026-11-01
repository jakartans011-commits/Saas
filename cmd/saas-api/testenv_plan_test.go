package main

import "testing"

// setTenantPlan mengubah PlanTier tenant yang sudah ada lewat store,
// melewati Stripe. Dipakai test yang perlu menguji perilaku per plan
// tanpa menjalankan alur checkout.
func (e *testEnv) setTenantPlan(t *testing.T, tenantID, tier string) {
	t.Helper()

	tn, err := e.tenantStore.Get(tenantID)
	if err != nil {
		t.Fatalf("get tenant %s: %v", tenantID, err)
	}
	tn.PlanTier = tier
	if err := e.tenantStore.Put(tn); err != nil {
		t.Fatalf("set plan %s for tenant %s: %v", tier, tenantID, err)
	}
}

// bootstrapTenantOwnerPro = bootstrapTenantOwner + plan Pro. Dipakai test yang
// menguji keamanan API key, bukan batas plan, supaya batas key Free (1 per
// tenant) tidak ikut menghalangi.
func (e *testEnv) bootstrapTenantOwnerPro(t *testing.T, tenantID, name, email, password string) {
	t.Helper()

	e.bootstrapTenantOwner(t, tenantID, name, email, password)
	e.setTenantPlan(t, tenantID, "pro")
}
