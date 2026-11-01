// core/permission/config.go
package permission

// PermissionConfig adalah konfigurasi permission
type PermissionConfig struct {
	ValidPermissions []string `json:"valid_permissions"`
	DefaultDeny      bool     `json:"default_deny"`
}

// DefaultPermissionConfig mengembalikan konfigurasi permission default
func DefaultPermissionConfig() *PermissionConfig {
	return &PermissionConfig{
		ValidPermissions: []string{
			"filesystem",
			"network",
			"process",
			"memory",
			"registry",
			"system",
			"storage",
			"crypto",
			"scheduler",
			"event",
			"hardware",
			"ai",
			"cloud",
			"browser",
			"thirdparty",
			"legacy",
			"test",
			"tenant", // ← TAMBAHKAN!
		},
		DefaultDeny: true,
	}
}
