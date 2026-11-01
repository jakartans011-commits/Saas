// auth/user_store_admin.go
package auth

// ============================================================
// File terpisah dari user.go SENGAJA — pola sama seperti
// rbac/role_store_admin.go: user.go yang sudah lolos TestAuthRBAC
// tidak disentuh sama sekali.
// ============================================================

// DeleteAllForTenant menghapus SEMUA user milik satu tenant.
// Dipanggil saat tenant di-deprovision — mencegah "user hantu"
// yang masih punya JWT valid tapi tenant-nya sudah tidak ada.
//
// Mengembalikan jumlah user yang dihapus.
func (s *InMemoryUserStore) DeleteAllForTenant(tenantID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for id, user := range s.users {
		if user.TenantID == tenantID {
			delete(s.users, id)
			deleted++
		}
	}
	return deleted
}

// CountForTenant — dipakai test untuk verifikasi sebelum/sesudah
// deprovision.
func (s *InMemoryUserStore) CountForTenant(tenantID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, user := range s.users {
		if user.TenantID == tenantID {
			count++
		}
	}
	return count
}
