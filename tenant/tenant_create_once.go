package tenant

import "errors"

// ErrTenantAlreadyExists dikembalikan oleh PutIfNotExists ketika tenant
// non-deleted dengan ID yang sama sudah ada.
var ErrTenantAlreadyExists = errors.New("tenant_already_exists")

// TenantCreator adalah interface terpisah (BUKAN menambah method ke
// TenantStore) untuk operasi atomic "create if not exists". Ini
// menghilangkan window Get-lalu-Put yang rentan race di ProvisionTenant.
type TenantCreator interface {
	PutIfNotExists(t *Tenant) error
}

// PutIfNotExists (InMemoryTenantStore): check dan write dalam SATU
// critical section, jadi dua goroutine tidak bisa sama-sama lolos
// pengecekan "belum ada" sebelum salah satu menulis.
func (s *InMemoryTenantStore) PutIfNotExists(t *Tenant) error {
	if t.ID == "" {
		return errors.New("tenant id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.tenants[t.ID]; ok && existing.DeletedAt == nil {
		return ErrTenantAlreadyExists
	}
	s.tenants[t.ID] = t
	return nil
}
