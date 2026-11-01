// auth/store_interfaces.go
package auth

// UserAdminStore adalah superset dari UserStore, menambah operasi
// yang dibutuhkan Tenant Management. Pola sama seperti
// rbac.RoleAdminStore — InMemoryUserStore sudah memenuhinya.
type UserAdminStore interface {
	UserStore
	Put(user *User) error
	DeleteAllForTenant(tenantID string) int
	CountForTenant(tenantID string) int
}
