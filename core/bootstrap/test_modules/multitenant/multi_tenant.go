// core/bootstrap/test_modules/multitenant/multi_tenant.go
package multitenant

import (
	"context"

	"trailseal/sdk"
)

// TenantModule adalah test module untuk memverifikasi
// pemisahan storage antar tenant.
type TenantModule struct {
	name     string
	tenantID string
	storage  sdk.Storage
}

func NewTenantModule(
	name string,
	tenantID string,
	storage sdk.Storage,
) *TenantModule {
	return &TenantModule{
		name:     name,
		tenantID: tenantID,
		storage:  storage,
	}
}

func (m *TenantModule) Name() string {
	return m.name
}

func (m *TenantModule) Version() string {
	return "1.0.0"
}

func (m *TenantModule) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:          m.name,
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		Category:      "tenant-test",
		Capabilities: []string{
			"tenant." + m.tenantID + ".read",
		},
		Permissions: []string{
			"storage",
		},
	}
}

func (m *TenantModule) Init(config sdk.Config) error {
	return nil
}

func (m *TenantModule) Start(ctx context.Context) error {
	if m.storage == nil {
		return nil
	}

	// Setiap tenant hanya menulis data ke storage yang
	// diberikan kepadanya.
	return m.storage.Save(
		"tenant_data",
		map[string]interface{}{
			"tenant_id": m.tenantID,
			"data":      "sensitive",
		},
	)
}

func (m *TenantModule) Stop() error {
	return nil
}

func (m *TenantModule) Health() error {
	return nil
}

func (m *TenantModule) SetStorage(storage sdk.Storage) {
	m.storage = storage
}
