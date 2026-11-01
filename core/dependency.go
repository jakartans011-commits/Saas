// core/dependency.go
package core

import "trailseal/sdk"

// DependencyAware adalah interface optional untuk plugin yang membutuhkan DI
type DependencyAware interface {
    SetEventBus(eventBus sdk.EventBus)
    SetLogger(logger sdk.Logger)
    SetStorage(storage sdk.Storage)
    SetCrypto(crypto sdk.Crypto)
    SetScheduler(scheduler sdk.Scheduler)
}