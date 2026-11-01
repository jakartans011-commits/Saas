// rbac/store_interfaces.go
package rbac

// RoleAdminStore adalah superset dari RoleStore, menambah operasi
// yang dibutuhkan Tenant Management (provisioning/deprovisioning).
// InMemoryRoleStore SUDAH memenuhi interface ini (structural typing
// Go) — tidak ada perubahan perilaku untuk kode yang sudah lolos test.
// Implementasi baru (mis. BoltRoleStore) tinggal memenuhi interface
// yang sama untuk bisa dipasang di tempat yang sama.
type RoleAdminStore interface {
	RoleStore
	PutRole(role *Role) error
	DeleteAllForTenant(tenantID string) int
	CountForTenant(tenantID string) int
}
