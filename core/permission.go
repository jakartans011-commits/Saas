// core/permission.go
package core

// Permission mendefinisikan izin yang dibutuhkan module
type Permission int

const (
	// PermNetwork - izin untuk akses jaringan
	PermNetwork Permission = 1 << iota // 1
	// PermFilesystem - izin untuk baca/tulis file
	PermFilesystem // 2
	// PermProcess - izin untuk melihat/manipulasi process
	PermProcess // 4
	// PermExecute - izin untuk menjalankan perintah
	PermExecute // 8
	// PermRegistry - izin untuk akses registry (Windows)
	PermRegistry // 16
	// PermSystem - izin untuk akses sistem level tinggi
	PermSystem // 32
)

// String returns string representation of permission
func (p Permission) String() string {
	switch p {
	case PermNetwork:
		return "Network"
	case PermFilesystem:
		return "Filesystem"
	case PermProcess:
		return "Process"
	case PermExecute:
		return "Execute"
	case PermRegistry:
		return "Registry"
	case PermSystem:
		return "System"
	default:
		return "Unknown"
	}
}

// HasPermission checks if permissions has a specific permission
func HasPermission(permissions []Permission, target Permission) bool {
	for _, p := range permissions {
		if p == target {
			return true
		}
	}
	return false
}

// PermissionSet represents a set of permissions
type PermissionSet struct {
	permissions []Permission
}

func NewPermissionSet(perms ...Permission) *PermissionSet {
	return &PermissionSet{
		permissions: perms,
	}
}

func (ps *PermissionSet) Add(p Permission) {
	ps.permissions = append(ps.permissions, p)
}

func (ps *PermissionSet) Has(p Permission) bool {
	return HasPermission(ps.permissions, p)
}

func (ps *PermissionSet) List() []Permission {
	return ps.permissions
}

func (ps *PermissionSet) String() string {
	var result string
	for i, p := range ps.permissions {
		if i > 0 {
			result += ", "
		}
		result += p.String()
	}
	return result
}
