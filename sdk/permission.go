// sdk/permission.go
package sdk

// Permission adalah konstanta permission yang tersedia
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
const (
	PermFilesystem = "filesystem"
	PermNetwork    = "network"
	PermProcess    = "process"
	PermMemory     = "memory"
	PermRegistry   = "registry"
	PermSystem     = "system"
	PermStorage    = "storage"
	PermCrypto     = "crypto"
	PermScheduler  = "scheduler"
	PermEvent      = "event"
)

// PermissionSet adalah helper untuk permission
type PermissionSet map[string]bool

func NewPermissionSet(perms []string) PermissionSet {
	ps := make(PermissionSet)
	for _, p := range perms {
		ps[p] = true
	}
	return ps
}

func (ps PermissionSet) Has(perm string) bool {
	return ps[perm]
}

func (ps PermissionSet) HasAll(perms []string) bool {
	for _, p := range perms {
		if !ps[p] {
			return false
		}
	}
	return true
}

// 🔥 STEP ⑥: PERMISSION SYSTEM — INTERFACE BARU

// PermissionEvaluator adalah interface untuk mengevaluasi permission
type PermissionEvaluator interface {
	// Evaluate mengevaluasi apakah permission diizinkan
	// Mengembalikan (allowed, reason)
	Evaluate(manifest Manifest, permission string) (bool, string)

	// EvaluateAll mengevaluasi semua permission di manifest
	// Mengembalikan daftar permission yang ditolak
	EvaluateAll(manifest Manifest) []string

	// IsAllowed mengecek apakah permission diizinkan (shortcut)
	IsAllowed(manifest Manifest, permission string) bool
}

// PermissionEnforcer adalah interface untuk enforcement spesifik domain
type PermissionEnforcer interface {
	// Enforce menegakkan permission untuk action tertentu
	// Mengembalikan error jika tidak diizinkan
	Enforce(manifest Manifest, action string, resource string) error

	// Name mengembalikan nama enforcer
	Name() string
}