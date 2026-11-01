// rbac/role_store_admin.go
package rbac

// ============================================================
// File terpisah dari role.go SENGAJA — supaya role.go yang sudah
// lolos TestAuthRBAC tidak disentuh sama sekali. Method di sini
// menambah kemampuan BARU pada InMemoryRoleStore yang sudah ada,
// tanpa mengubah satu baris pun kode yang sudah terbukti bekerja.
// ============================================================

// DeleteAllForTenant menghapus SEMUA role milik satu tenant.
// Dipanggil saat tenant di-deprovision — mencegah "role hantu"
// (Section: capability/permission hantu yang sudah kita bahas)
// tetap ada di store setelah tenant-nya sendiri sudah dihapus.
//
// Mengembalikan jumlah role yang dihapus, untuk keperluan audit log
// dan assertion di test.
func (s *InMemoryRoleStore) DeleteAllForTenant(tenantID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for key, role := range s.roles {
		if role.TenantID == tenantID {
			delete(s.roles, key)
			deleted++
		}
	}
	return deleted
}

// CountForTenant — dipakai test untuk verifikasi sebelum/sesudah
// deprovision, tanpa perlu expose seluruh map.
func (s *InMemoryRoleStore) CountForTenant(tenantID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, role := range s.roles {
		if role.TenantID == tenantID {
			count++
		}
	}
	return count
}