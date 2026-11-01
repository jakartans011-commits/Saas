// core/adapter/registry_adapter.go
package adapter

import (
	"fmt"
	"sync"

	"trailseal/sdk"
)

// RegistryAdapter adalah adapter untuk registry (legacy)
type RegistryAdapter struct {
	mu        sync.RWMutex
	registry  map[string]func(sdk.Container) sdk.Plugin
	manifests map[string]sdk.Manifest
}

// NewRegistryAdapter membuat adapter baru
func NewRegistryAdapter() sdk.Registry {
	return &RegistryAdapter{
		registry: make(map[string]func(sdk.Container) sdk.Plugin),
	}
}

// ============================================================
// BASIC REGISTRATION
// ============================================================

func (r *RegistryAdapter) Register(name string, factory func(sdk.Container) sdk.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.registry[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	r.registry[name] = factory
	return nil
}

func (r *RegistryAdapter) RegisterManifest(name string, manifest sdk.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.manifests[name]; exists {
		return fmt.Errorf("plugin manifest %s already registered", name)
	}

	r.manifests[name] = manifest
	return nil
}

// ============================================================
// RETRIEVAL
// ============================================================

func (r *RegistryAdapter) Get(name string) (func(sdk.Container) sdk.Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, exists := r.registry[name]
	return factory, exists
}

func (r *RegistryAdapter) GetManifest(name string) (sdk.Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manifest, exists := r.manifests[name]
	return manifest, exists
}

// ============================================================
// LISTING
// ============================================================

func (r *RegistryAdapter) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.registry))
	for name := range r.registry {
		names = append(names, name)
	}
	return names
}

func (r *RegistryAdapter) ListManifests() []sdk.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manifests := make([]sdk.Manifest, 0, len(r.manifests))
	for _, manifest := range r.manifests {
		manifests = append(manifests, manifest)
	}

	return manifests
}

// ============================================================
// 🔥 STEP ⑦: REGISTRY FINAL — QUERY METHODS (ADAPTER KOMPATIBILITAS)
// ============================================================

func (r *RegistryAdapter) ListByCategory(category string) []sdk.Manifest {
	return []sdk.Manifest{}
}

func (r *RegistryAdapter) ListByCapability(pattern string) []sdk.Manifest {
	return []sdk.Manifest{}
}

func (r *RegistryAdapter) ListByPermission(permission string) []sdk.Manifest {
	return []sdk.Manifest{}
}

func (r *RegistryAdapter) ListByRequirement(req string) []sdk.Manifest {
	return []sdk.Manifest{}
}

func (r *RegistryAdapter) Stats() sdk.RegistryStats {
	return sdk.RegistryStats{
		TotalPlugins:   len(r.registry),
		TotalManifests: 0,
		Categories:     make(map[string]int),
		Permissions:    make(map[string]int),
		Capabilities:   make(map[string]int),
	}
}

// ============================================================
// UNREGISTER & HAS
// ============================================================

func (r *RegistryAdapter) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.registry[name]; !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	delete(r.registry, name)
	delete(r.manifests, name)

	return nil
}

func (r *RegistryAdapter) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.registry[name]
	return exists
}

// ============================================================
// STEP ⑤: CAPABILITY SYSTEM
// ============================================================

func (r *RegistryAdapter) HasCapability(name string, pattern string) bool {
	return false
}

func (r *RegistryAdapter) GetCapabilities(name string) []string {
	return []string{}
}

// ============================================================
// STEP ⑥: PERMISSION SYSTEM
// ============================================================

func (r *RegistryAdapter) HasPermission(name string, permission string) bool {
	return false
}
