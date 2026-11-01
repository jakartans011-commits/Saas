// sdk/registry.go
package sdk

// Registry adalah interface untuk plugin registry
type Registry interface {
	// ============================================================
	// BASIC REGISTRATION
	// ============================================================
	Register(name string, factory func(Container) Plugin) error
	RegisterManifest(name string, manifest Manifest) error

	// ============================================================
	// RETRIEVAL
	// ============================================================
	Get(name string) (func(Container) Plugin, bool)
	GetManifest(name string) (Manifest, bool)

	// ============================================================
	// LISTING
	// ============================================================
	List() []string
	ListManifests() []Manifest

	// ============================================================
	// 🔥 STEP ⑦: REGISTRY FINAL — QUERY METHODS
	// ============================================================
	ListByCategory(category string) []Manifest
	ListByCapability(pattern string) []Manifest
	ListByPermission(permission string) []Manifest
	ListByRequirement(req string) []Manifest

	// ============================================================
	// STATISTICS
	// ============================================================
	Stats() RegistryStats

	// ============================================================
	// UNREGISTER & HAS
	// ============================================================
	Unregister(name string) error
	Has(name string) bool

	// ============================================================
	// STEP ⑤: CAPABILITY SYSTEM
	// ============================================================
	HasCapability(name string, pattern string) bool
	GetCapabilities(name string) []string

	// ============================================================
	// STEP ⑥: PERMISSION SYSTEM
	// ============================================================
	HasPermission(name string, permission string) bool
}

// RegistryStats adalah statistik registry
type RegistryStats struct {
	TotalPlugins   int            `json:"total_plugins"`
	TotalManifests int            `json:"total_manifests"`
	Categories     map[string]int `json:"categories"`
	Permissions    map[string]int `json:"permissions"`
	Capabilities   map[string]int `json:"capabilities"`
}
