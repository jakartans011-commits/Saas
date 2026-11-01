// sdk/plugin.go
package sdk

import "context"

// Plugin adalah kontrak dasar untuk semua module
// SEMUA module WAJIB mengimplementasikan interface ini
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Plugin interface {
	// Identity
	Name() string
	Version() string
	Manifest() Manifest

	// Lifecycle - WAJIB!
	Init(config Config) error
	Start(ctx context.Context) error
	Stop() error
	Health() error
}

// Manifest berisi metadata module
type Manifest struct {
	// ============================================================
	// EXISTING FIELDS
	// ============================================================
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Author       string   `json:"author"`
	Description  string   `json:"description"`
	Permissions  []string `json:"permissions"`
	Dependencies []string `json:"dependencies"`
	Priority     int      `json:"priority"`
	Category     string   `json:"category"`
	Capabilities []string `json:"capabilities"`
	Requirements []string `json:"requirements"`
	MinSDKVersion string  `json:"min_sdk_version"`
	MaxSDKVersion string  `json:"max_sdk_version"`
	Weight       int      `json:"weight"`

	// ============================================================
	// 🔥 NEW FIELDS (STEP ④: MANIFEST VALIDATION)
	// ============================================================
	SchemaVersion string `json:"schema_version"`              // "1.0.0"
	Signature     string `json:"signature,omitempty"`         // Untuk supply chain security
	Publisher     string `json:"publisher,omitempty"`         // Untuk marketplace
	Readme        string `json:"readme,omitempty"`            // Untuk discoverability
	
	// 🔥 NEW: TRIGGER DECLARATIONS (STEP 13)
	// ============================================================
	Triggers []TriggerDeclaration `json:"triggers,omitempty"`
}

type PluginFactory func(container Container) Plugin