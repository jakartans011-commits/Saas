// rbac/role.go
package rbac

import (
	"fmt"
	"sync"
)

// ============================================================
// ROLE — WAJIB TENANT-SCOPED (Section 8-9)
//
// Role "admin" milik Tenant A dan Role "admin" milik Tenant B
// adalah dua object berbeda secara security boundary, walau
// namanya sama. TenantID WAJIB diverifikasi di setiap resolusi.
// ============================================================

type Role struct {
	ID               string
	TenantID         string
	Name             string
	CapabilityGrants []string // pattern, mis. "tenant.tenant-a.*"
}

// ============================================================
// ROLE STORE — in-memory untuk Fase 1 (lihat catatan di atas).
// Interface terpisah dari implementasi supaya bisa diganti ke
// bbolt/SQL nanti tanpa mengubah RoleResolver/RBAC logic.
// ============================================================

type RoleStore interface {
	// GetRole mengembalikan role HANYA jika role.TenantID == tenantID.
	// Kalau role exist tapi milik tenant lain, WAJIB return error,
	// BUKAN role tersebut — ini mencegah role_id collision bug
	// (Section 17).
	GetRole(tenantID, roleID string) (*Role, error)
	PutRole(role *Role) error
}

type InMemoryRoleStore struct {
	mu    sync.RWMutex
	roles map[string]*Role // key: tenantID + ":" + roleID
}

func NewInMemoryRoleStore() *InMemoryRoleStore {
	return &InMemoryRoleStore{
		roles: make(map[string]*Role),
	}
}

func roleKey(tenantID, roleID string) string {
	return tenantID + ":" + roleID
}

func (s *InMemoryRoleStore) PutRole(role *Role) error {
	if role.TenantID == "" {
		return fmt.Errorf("role %s: tenant_id is required, cannot store global role", role.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[roleKey(role.TenantID, role.ID)] = role
	return nil
}

func (s *InMemoryRoleStore) GetRole(tenantID, roleID string) (*Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	role, ok := s.roles[roleKey(tenantID, roleID)]
	if !ok {
		// PENTING: JANGAN cek "apakah roleID exist di tenant manapun"
		// lalu return itu. Kalau key tidak exact match tenantID:roleID,
		// ini harus dianggap "tidak ada role ini DI TENANT INI",
		// bukan "role tidak pernah ada sama sekali". Behaviour ini
		// yang mencegah role_id collision bug di Section 17.
		return nil, fmt.Errorf(
			"role_resolution_failed: role %s not found for tenant %s",
			roleID, tenantID,
		)
	}
	return role, nil
}

// ============================================================
// ROLE RESOLVER — dipanggil middleware/RBAC entry point
// ============================================================

type RoleResolver struct {
	store RoleStore
}

func NewRoleResolver(store RoleStore) *RoleResolver {
	return &RoleResolver{store: store}
}

// ResolveCapabilities mengembalikan capability grants untuk role,
// dengan verifikasi EKSPLISIT bahwa role.TenantID == tenantID yang
// diminta (Section 17) — pertahanan berlapis, walau GetRole di atas
// sudah scoped by key, kita verifikasi ULANG field TenantID di
// object yang dikembalikan, supaya kalau implementasi RoleStore lain
// (mis. bbolt/SQL nanti) punya bug query, ini tetap tertangkap di sini.
func (r *RoleResolver) ResolveCapabilities(tenantID, roleID string) ([]string, error) {
	role, err := r.store.GetRole(tenantID, roleID)
	if err != nil {
		return nil, err
	}

	if role.TenantID != tenantID {
		// Defense in depth — seharusnya tidak pernah tercapai kalau
		// RoleStore benar, tapi jangan pernah percaya buta.
		return nil, fmt.Errorf(
			"tenant_mismatch: role %s belongs to tenant %s, not %s",
			roleID, role.TenantID, tenantID,
		)
	}

	return role.CapabilityGrants, nil
}
