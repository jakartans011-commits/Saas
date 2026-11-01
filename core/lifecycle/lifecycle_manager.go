// core/lifecycle/lifecycle_manager.go
package lifecycle

import (
	"trailseal/sdk"
)

// LifecycleManager mengelola lifecycle semua plugin
type LifecycleManager struct {
	logger    sdk.Logger
	eventBus  sdk.EventBus
}

// NewLifecycleManager membuat lifecycle manager baru
func NewLifecycleManager(logger sdk.Logger, eventBus sdk.EventBus) *LifecycleManager {
	return &LifecycleManager{
		logger:   logger,
		eventBus: eventBus,
	}
}