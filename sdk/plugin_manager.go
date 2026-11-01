// sdk/plugin_manager.go
package sdk

// PluginManager adalah interface untuk plugin manager
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type PluginManager interface {
	// LoadAll memuat semua plugin dari registry
	LoadAll() error

	// ValidateAll memvalidasi semua plugin
	ValidateAll() error

	// StartAll memulai semua plugin
	StartAll() error

	// StopAll menghentikan semua plugin
	StopAll() error

	// HealthCheck memeriksa kesehatan semua plugin
	HealthCheck() map[string]error

	// GetPlugin mengembalikan instance plugin
	GetPlugin(name string) (interface{}, bool)

	// ListPlugins mengembalikan semua nama plugin
	ListPlugins() []string

	// ListRunning mengembalikan plugin yang sedang berjalan
	ListRunning() []string

	// GetLoadOrder mengembalikan urutan load plugin
	GetLoadOrder() []string

	// GetPluginInfo mengembalikan informasi lengkap plugin
	GetPluginInfo(name string) map[string]interface{}

	// EnablePlugin mengaktifkan plugin
	EnablePlugin(name string) error

	// DisablePlugin menonaktifkan plugin
	DisablePlugin(name string) error

	// IsEnabled memeriksa apakah plugin aktif
	IsEnabled(name string) bool

	// RegisterExecutable mendaftarkan executable ke scheduler
	RegisterExecutable(executable Executable) error

	// UnregisterExecutable menghapus executable dari scheduler
	UnregisterExecutable(name string) error

	// TriggerExecutable secara manual menjalankan executable
	TriggerExecutable(name string) error
}
