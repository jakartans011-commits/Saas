package tenant

// TenantActiveLister — interface terpisah, bukan tambahan ke TenantStore.
//
// Mengikuti instruksi di tenant.go: "JANGAN dihapus/diedit manual — kalau
// perlu method tambahan, buat interface terpisah di file lain", pola yang
// sama dengan TenantPastDueLister dan TenantByStripeLookup.
//
// BoltTenantStore sudah memilikinya (tenant_store_active.go); yang kurang
// hanya interface untuk type assertion, dan implementasi in-memory supaya
// rekonsiliasi Stripe bisa diuji tanpa bbolt.
type TenantActiveLister interface {
	ListActive() ([]*Tenant, error)
}

// ListActive (InMemoryTenantStore) — tenant yang belum soft-deleted.
//
// Urutan map di Go acak dan itu dibiarkan: pemanggil (rekonsiliasi Stripe)
// memproses setiap tenant secara independen, jadi urutan tidak boleh
// memengaruhi hasil. Kalau suatu saat urutan jadi penting, itu tanda ada
// bug di pemanggilnya, bukan di sini.
func (s *InMemoryTenantStore) ListActive() ([]*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		if t == nil || t.DeletedAt != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
