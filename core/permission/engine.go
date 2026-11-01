// core/permission/engine.go
package permission

import (
	"fmt"
	"strings"
	"sync"

	"trailseal/sdk"
)

// Engine adalah Permission Engine dengan default-deny policy
type Engine struct {
	mu          sync.RWMutex
	enforcers   map[string]sdk.PermissionEnforcer
	defaultDeny bool
	config      *PermissionConfig
}

// NewEngine membuat permission engine baru
func NewEngine() *Engine {
	return NewEngineWithConfig(DefaultPermissionConfig())
}

// NewEngineWithConfig membuat permission engine dengan config
func NewEngineWithConfig(config *PermissionConfig) *Engine {
	if config == nil {
		config = DefaultPermissionConfig()
	}
	return &Engine{
		enforcers:   make(map[string]sdk.PermissionEnforcer),
		defaultDeny: config.DefaultDeny,
		config:      config,
	}
}

// SetConfig mengubah konfigurasi permission
func (e *Engine) SetConfig(config *PermissionConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
	e.defaultDeny = config.DefaultDeny
}

// SetDefaultDeny mengubah policy default (true = deny, false = allow)
func (e *Engine) SetDefaultDeny(deny bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.defaultDeny = deny
}

// RegisterEnforcer mendaftarkan permission enforcer
func (e *Engine) RegisterEnforcer(enforcer sdk.PermissionEnforcer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enforcers[enforcer.Name()] = enforcer
}

// GetEnforcer mengembalikan enforcer berdasarkan nama
func (e *Engine) GetEnforcer(name string) (sdk.PermissionEnforcer, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	enf, ok := e.enforcers[name]
	return enf, ok
}

// Evaluate mengevaluasi apakah permission diizinkan
func (e *Engine) Evaluate(manifest sdk.Manifest, permission string) (bool, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// STEP 1: Cek apakah permission ada di manifest
	hasPermission := false
	for _, p := range manifest.Permissions {
		if strings.EqualFold(p, permission) {
			hasPermission = true
			break
		}
	}

	// STEP 2: Jika tidak ada di manifest → DENY (default-deny)
	if !hasPermission {
		return false, fmt.Sprintf("permission %s not declared in manifest", permission)
	}

	// STEP 3: Jika ada di manifest → cek enforcer
	for _, enforcer := range e.enforcers {
		if err := enforcer.Enforce(manifest, "check", permission); err != nil {
			return false, fmt.Sprintf("enforcer %s denied: %v", enforcer.Name(), err)
		}
	}

	// STEP 4: Jika semua lolos → ALLOW
	return true, "allowed"
}

// EvaluateAll mengevaluasi semua permission di manifest
func (e *Engine) EvaluateAll(manifest sdk.Manifest) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	denied := []string{}
	for _, perm := range manifest.Permissions {
		allowed, _ := e.Evaluate(manifest, perm)
		if !allowed {
			denied = append(denied, perm)
		}
	}
	return denied
}

// IsAllowed mengecek apakah permission diizinkan (shortcut)
func (e *Engine) IsAllowed(manifest sdk.Manifest, permission string) bool {
	allowed, _ := e.Evaluate(manifest, permission)
	return allowed
}

// ValidateManifestPermissions memvalidasi semua permission di manifest
func (e *Engine) ValidateManifestPermissions(manifest sdk.Manifest) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 🔥 PAKAI CONFIG — BUKAN HARDCODE!
	if e.config == nil {
		e.config = DefaultPermissionConfig()
	}

	validPerms := make(map[string]bool)
	for _, p := range e.config.ValidPermissions {
		validPerms[p] = true
	}

	for _, perm := range manifest.Permissions {
		if !validPerms[strings.ToLower(perm)] {
			return fmt.Errorf("invalid permission: %s", perm)
		}
	}

	// Cek default-deny policy
	denied := e.EvaluateAll(manifest)
	if len(denied) > 0 {
		return fmt.Errorf("permissions denied by default-deny policy: %v", denied)
	}

	return nil
}

// GetValidPermissions mengembalikan daftar permission yang valid
func (e *Engine) GetValidPermissions() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.config == nil {
		return DefaultPermissionConfig().ValidPermissions
	}
	return e.config.ValidPermissions
}
