package main

import "trailseal/billing"

// apiKeyLimit mengembalikan batas key aktif per tenant dari PlanConfig
// (0 = tanpa batas).
//
// Fail closed ke Free di SEMUA jalur yang tidak pasti: plan tidak dikenal,
// tenant gagal dibaca, atau store belum terpasang. Versi sebelumnya
// memulangkan 0 (tanpa batas) ketika store nil — default yang melewati
// batas plan, berlawanan dengan yang dijanjikan komentarnya sendiri.
func (s PortalStores) apiKeyLimit(tenantID string) int {
	if s.PlansRegistry == nil {
		// Tanpa registry tidak ada PlanConfig yang bisa dibaca. Nilai Free
		// menurut tabel pricing adalah 1 key aktif.
		return 1
	}

	tier := billing.PlanFree
	if s.TenantStore != nil {
		if t, err := s.TenantStore.Get(tenantID); err == nil && t.PlanTier != "" {
			tier = billing.PlanTier(t.PlanTier)
		}
	}

	plan, ok := s.PlansRegistry.Get(tier)
	if !ok {
		plan = s.PlansRegistry.Free()
	}
	return plan.MaxActiveAPIKeys
}
