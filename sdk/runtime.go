// sdk/runtime.go
package sdk

// Runtime adalah interface untuk runtime engine
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Runtime interface {
	LoadPlugins() error
	StartAll() error
	StopAll() error
	GetPlugin(name string) (Plugin, bool)
	ListPlugins() []string
	HealthCheck() map[string]error
	GetEventBus() EventBus
	GetLogger() Logger
}