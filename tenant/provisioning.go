// tenant/provisioning.go
package tenant

import (
	"errors"
	"time"

	"trailseal/auth"
	"trailseal/rbac"
)

// ============================================================
// PROVISION
// ============================================================

const defaultOwnerRoleID = "owner"

// ProvisionTenant membuat tenant baru + role default "owner" dengan
// capability grant penuh DI DALAM tenant tersebut (tenant.<id>.*).
//
// Tenant yang SUDAH ADA (status apapun) menolak provisioning ulang —
// ini sengaja ketat untuk mencegah tenant lama ter-revive diam-diam
// dengan asosiasi role/user yang campur aduk dengan yang baru.
func ProvisionTenant(
	tenantStore TenantStore,
	roleStore rbac.RoleAdminStore,
	tenantID, name string,
) (*Tenant, error) {
	if tenantID == "" || name == "" {
		return nil, errors.New("tenant_id and name are required")
	}

	creator, ok := tenantStore.(TenantCreator)
	if !ok {
		return nil, errors.New("tenant_store_does_not_support_atomic_create")
	}

	t := &Tenant{
		ID:        tenantID,
		Name:      name,
		Status:    TenantStatusActive,
		CreatedAt: time.Now(),
	}

	if err := creator.PutIfNotExists(t); err != nil {
		return nil, err // termasuk ErrTenantAlreadyExists
	}

	ownerRole := &rbac.Role{
		ID:               defaultOwnerRoleID,
		TenantID:         tenantID,
		Name:             "owner",
		CapabilityGrants: []string{"tenant." + tenantID + ".*"},
	}
	if err := roleStore.PutRole(ownerRole); err != nil {
		// Rollback: tenant record sudah terlanjur tertulis tapi role
		// owner gagal dibuat. Tandai deprovisioned supaya tidak jadi
		// tenant "hantu" (aktif tapi tanpa owner role sama sekali).
		_ = tenantStore.Put(&Tenant{
			ID: tenantID, Name: name,
			Status: TenantStatusDeprovisioned, CreatedAt: t.CreatedAt,
		})
		return nil, err
	}

	return t, nil
}

// ============================================================
// DEPROVISION
// ============================================================

type DeprovisionResult struct {
	TenantID     string
	UsersDeleted int
	RolesDeleted int
}

// DeprovisionTenant menghapus SEMUA role dan user milik tenant,
// lalu menandai tenant sebagai deprovisioned. Urutan WAJIB:
// hapus user & role DULU, baru ubah status tenant — supaya kalau
// proses ini gagal di tengah jalan, tenant tetap "active" (fail-safe),
// bukan "deprovisioned" tapi user/role-nya masih hidup (fail-unsafe,
// itu skenario "hantu" yang ingin kita hindari).
func DeprovisionTenant(
	tenantStore TenantStore,
	roleStore rbac.RoleAdminStore,
	userStore auth.UserAdminStore,
	tenantID string,
) (*DeprovisionResult, error) {
	t, err := tenantStore.Get(tenantID)
	if err != nil {
		return nil, errors.New("tenant_not_found")
	}
	if t.Status == TenantStatusDeprovisioned {
		return nil, errors.New("tenant_already_deprovisioned")
	}

	usersDeleted := userStore.DeleteAllForTenant(tenantID)
	rolesDeleted := roleStore.DeleteAllForTenant(tenantID)

	t.Status = TenantStatusDeprovisioned
	if err := tenantStore.Put(t); err != nil {
		// Users/roles sudah terlanjur terhapus di titik ini — state
		// tidak sepenuhnya konsisten kalau Put gagal. Untuk in-memory
		// store ini praktis tidak akan terjadi, tapi di implementasi
		// DB nyata (bbolt/SQL) nanti, WAJIB dibungkus transaksi supaya
		// ketiga operasi ini atomic. Dicatat sebagai TODO eksplisit,
		// bukan diabaikan.
		return nil, errors.New("tenant_status_update_failed_after_cleanup")
	}

	return &DeprovisionResult{
		TenantID:     tenantID,
		UsersDeleted: usersDeleted,
		RolesDeleted: rolesDeleted,
	}, nil
}