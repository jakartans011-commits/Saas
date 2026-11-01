// core/registry/registry.go
package registry

import (
	"fmt"
	"strings"
	"sync"

	"trailseal/core/capability"
	"trailseal/core/contract"
	"trailseal/core/manifest"
	"trailseal/core/permission"
	"trailseal/sdk"
)

// registryImpl adalah implementasi Registry
type registryImpl struct {
	mu               sync.RWMutex
	plugins          map[string]func(sdk.Container) sdk.Plugin
	manifests        map[string]sdk.Manifest
	manifestProc     *manifest.Processor
	capabilityEngine *capability.Engine
	permEngine       *permission.Engine
	contractGate     *contract.ContractGate
}

// NewRegistry membuat registry baru
func NewRegistry() sdk.Registry {
	return &registryImpl{
		plugins:          make(map[string]func(sdk.Container) sdk.Plugin),
		manifests:        make(map[string]sdk.Manifest),
		manifestProc:     manifest.NewProcessor(),
		capabilityEngine: capability.NewEngine(),
		permEngine:       permission.NewEngine(),
		contractGate:     nil,
	}
}

// ============================================================
// GETTERS (untuk internal use)
// ============================================================

func (r *registryImpl) GetManifestProcessor() *manifest.Processor {
	return r.manifestProc
}

func (r *registryImpl) GetCapabilityEngine() *capability.Engine {
	return r.capabilityEngine
}

func (r *registryImpl) GetPermissionEngine() *permission.Engine {
	return r.permEngine
}

// ============================================================
// 🔥 STEP 15.5: CONTRACT TEST GATE
// ============================================================

// SetContractGate mengatur contract gate
func (r *registryImpl) SetContractGate(gate *contract.ContractGate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contractGate = gate
}

// GetContractGate mengembalikan contract gate
func (r *registryImpl) GetContractGate() *contract.ContractGate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.contractGate
}

// ============================================================
// BASIC REGISTRATION — DENGAN CONTRACT TEST GATE!
// ============================================================

func (r *registryImpl) Register(name string, factory func(sdk.Container) sdk.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	// 🔥 STEP 15.5: CONTRACT TEST GATE
	if r.contractGate != nil {
		plugin := factory(nil)
		if plugin == nil {
			return fmt.Errorf("plugin factory returned nil for %s", name)
		}

		result := r.contractGate.VerifyPlugin(plugin)
		if !result.Passed {
			return fmt.Errorf("contract test failed for %s: %v", name, result.Errors)
		}

		if result.HighAssurance {
			// Log untuk audit
		}
	}

	r.plugins[name] = factory
	return nil
}

func (r *registryImpl) RegisterManifest(name string, raw sdk.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.manifests[name]; exists {
		return fmt.Errorf("manifest %s already registered", name)
	}

	// STEP ④: VALIDASI + NORMALISASI + ALIAS RESOLUTION
	processed, err := r.manifestProc.ProcessManifest(raw)
	if err != nil {
		return fmt.Errorf("manifest processing failed: %w", err)
	}

	// STEP ⑤: CAPABILITY ENGINE — Validasi capability dengan matcher
	if len(processed.Capabilities) > 0 {
		if !r.capabilityEngine.MatchAnyCapability(processed.Capabilities, []string{"*"}) {
			return fmt.Errorf("capability engine validation failed for manifest %s", name)
		}
	}

	// STEP ⑥: PERMISSION SYSTEM — Validasi permission dengan default-deny
	if err := r.permEngine.ValidateManifestPermissions(processed); err != nil {
		return fmt.Errorf("permission validation failed: %w", err)
	}

	r.manifests[name] = processed
	return nil
}

// ============================================================
// RETRIEVAL
// ============================================================

func (r *registryImpl) Get(name string) (func(sdk.Container) sdk.Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.plugins[name]
	return f, ok
}

func (r *registryImpl) GetManifest(name string) (sdk.Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.manifests[name]
	return m, ok
}

// ============================================================
// LISTING
// ============================================================

func (r *registryImpl) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

func (r *registryImpl) ListManifests() []sdk.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]sdk.Manifest, 0, len(r.manifests))
	for _, m := range r.manifests {
		result = append(result, m)
	}
	return result
}

// ============================================================
// 🔥 STEP ⑦: REGISTRY FINAL — QUERY METHODS
// ============================================================

// ListByCategory mengembalikan semua manifest dengan category tertentu
func (r *registryImpl) ListByCategory(category string) []sdk.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := []sdk.Manifest{}
	category = strings.ToLower(strings.TrimSpace(category))

	for _, m := range r.manifests {
		if strings.EqualFold(m.Category, category) {
			result = append(result, m)
		}
	}
	return result
}

// ListByCapability mengembalikan semua manifest yang memiliki capability tertentu
func (r *registryImpl) ListByCapability(pattern string) []sdk.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := []sdk.Manifest{}
	for _, m := range r.manifests {
		if r.capabilityEngine.MatchAnyCapability(m.Capabilities, []string{pattern}) {
			result = append(result, m)
		}
	}
	return result
}

// ListByPermission mengembalikan semua manifest yang memiliki permission tertentu
func (r *registryImpl) ListByPermission(permission string) []sdk.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := []sdk.Manifest{}
	permission = strings.ToLower(strings.TrimSpace(permission))

	for _, m := range r.manifests {
		for _, p := range m.Permissions {
			if strings.EqualFold(p, permission) {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

// ListByRequirement mengembalikan semua manifest yang memiliki requirement tertentu
func (r *registryImpl) ListByRequirement(req string) []sdk.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := []sdk.Manifest{}
	req = strings.ToLower(strings.TrimSpace(req))

	for _, m := range r.manifests {
		for _, rq := range m.Requirements {
			if strings.EqualFold(rq, req) {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

// ============================================================
// STATISTICS
// ============================================================

// Stats mengembalikan statistik registry
func (r *registryImpl) Stats() sdk.RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := sdk.RegistryStats{
		TotalPlugins:   len(r.plugins),
		TotalManifests: len(r.manifests),
		Categories:     make(map[string]int),
		Permissions:    make(map[string]int),
		Capabilities:   make(map[string]int),
	}

	for _, m := range r.manifests {
		if m.Category != "" {
			stats.Categories[m.Category]++
		}

		for _, p := range m.Permissions {
			stats.Permissions[p]++
		}

		for _, c := range m.Capabilities {
			stats.Capabilities[c]++
		}
	}

	return stats
}

// ============================================================
// UNREGISTER & HAS
// ============================================================

func (r *registryImpl) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	delete(r.plugins, name)
	delete(r.manifests, name)
	return nil
}

func (r *registryImpl) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.plugins[name]
	return ok
}

// ============================================================
// STEP ⑤: CAPABILITY SYSTEM
// ============================================================

func (r *registryImpl) HasCapability(name string, pattern string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manifest, ok := r.manifests[name]
	if !ok {
		return false
	}

	return r.capabilityEngine.MatchAnyCapability(manifest.Capabilities, []string{pattern})
}

func (r *registryImpl) GetCapabilities(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manifest, ok := r.manifests[name]
	if !ok {
		return []string{}
	}

	return manifest.Capabilities
}

// ============================================================
// STEP ⑥: PERMISSION SYSTEM
// ============================================================

func (r *registryImpl) HasPermission(name string, permission string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manifest, ok := r.manifests[name]
	if !ok {
		return false
	}

	return r.permEngine.IsAllowed(manifest, permission)
}