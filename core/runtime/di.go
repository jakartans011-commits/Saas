// core/runtime/di.go
package runtime

import (
	"trailseal/sdk"
)

// PluginDI adalah interface untuk plugin yang menerima dependency injection
type PluginDI interface {
	SetEventBus(eventBus sdk.EventBus)
	SetLogger(logger sdk.Logger)
	SetStorage(storage sdk.Storage)
	SetCrypto(crypto sdk.Crypto)
	SetScheduler(scheduler sdk.Scheduler)
}

// InjectDependencies meng-inject dependency ke plugin
func InjectDependencies(plugin sdk.Plugin, deps *Dependencies) {
	if di, ok := plugin.(PluginDI); ok {
		if deps.EventBus != nil {
			di.SetEventBus(deps.EventBus)
		}
		if deps.Logger != nil {
			di.SetLogger(deps.Logger)
		}
		if deps.Storage != nil {
			di.SetStorage(deps.Storage)
		}
		if deps.Crypto != nil {
			di.SetCrypto(deps.Crypto)
		}
		if deps.Scheduler != nil {
			di.SetScheduler(deps.Scheduler)
		}
	}
}

// Dependencies adalah kumpulan dependency untuk plugin
type Dependencies struct {
	EventBus  sdk.EventBus
	Logger    sdk.Logger
	Storage   sdk.Storage
	Crypto    sdk.Crypto
	Scheduler sdk.Scheduler
}
