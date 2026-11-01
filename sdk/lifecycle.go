// sdk/lifecycle.go
package sdk

import "context"

// Lifecycle adalah interface untuk lifecycle management
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Lifecycle interface {
	// Init initializes the plugin
	Init(ctx context.Context) error

	// Start starts the plugin
	Start(ctx context.Context) error

	// Stop stops the plugin
	Stop(ctx context.Context) error

	// Destroy destroys the plugin
	Destroy(ctx context.Context) error

	// Health checks plugin health
	Health() error

	// State returns current state
	State() LifecycleState
}

// LifecycleState adalah state plugin
type LifecycleState string

const (
	StateCreated  LifecycleState = "created"
	StateInit     LifecycleState = "init"
	StateValid    LifecycleState = "valid"
	StateInvalid  LifecycleState = "invalid"
	StateStarting LifecycleState = "starting"
	StateRunning  LifecycleState = "running"
	StateStopping LifecycleState = "stopping"
	StateStopped  LifecycleState = "stopped"
	StateDestroy  LifecycleState = "destroy"
	StateError    LifecycleState = "error"
)
