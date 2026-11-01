// core/permission/enforcer.go
package permission

import (
	"fmt"
	"strings"

	"trailseal/sdk"
)

// DefaultEnforcer adalah implementasi PermissionEnforcer default
type DefaultEnforcer struct {
	name string
}

// NewDefaultEnforcer membuat default enforcer
func NewDefaultEnforcer() *DefaultEnforcer {
	return &DefaultEnforcer{
		name: "default",
	}
}

// Name mengembalikan nama enforcer
func (e *DefaultEnforcer) Name() string {
	return e.name
}

// Enforce menegakkan permission
func (e *DefaultEnforcer) Enforce(manifest sdk.Manifest, action string, resource string) error {
	// Cek apakah permission ada di manifest
	for _, perm := range manifest.Permissions {
		if strings.EqualFold(perm, resource) {
			return nil // allowed
		}
	}
	return fmt.Errorf("permission %s not granted for action %s", resource, action)
}