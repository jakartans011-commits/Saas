// sdk/container.go
package sdk

// ContainerScope adalah scope lifecycle service
type ContainerScope string

const (
	// ScopeSingleton — satu instance untuk seluruh container
	ScopeSingleton ContainerScope = "singleton"
	// ScopeModule — satu instance per module
	ScopeModule ContainerScope = "module"
	// ScopeRequest — satu instance per request
	ScopeRequest ContainerScope = "request"
	// ScopeTenant — satu instance per tenant
	ScopeTenant ContainerScope = "tenant"
)

// ServiceDefinition adalah definisi service di container
type ServiceDefinition struct {
	Name        string
	Service     interface{}
	Scope       ContainerScope
	Factory     func(Container) interface{} // Factory untuk create instance
	IsSingleton bool
	IsReady     bool
}

// Container adalah service container untuk dependency injection
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Container interface {
	// ============================================================
	// BASIC OPERATIONS
	// ============================================================
	
	// Get mengambil service dari container
	Get(name string) interface{}

	// Register mendaftarkan service ke container
	Register(name string, service interface{})

	// RegisterWithScope mendaftarkan service dengan scope
	RegisterWithScope(name string, service interface{}, scope ContainerScope)

	// RegisterFactory mendaftarkan service dengan factory
	RegisterFactory(name string, factory func(Container) interface{}, scope ContainerScope)

	// ============================================================
	// SERVICE RETRIEVAL (type-safe)
	// ============================================================

	// GetLogger mengambil logger
	GetLogger() Logger

	// GetEventBus mengambil event bus
	GetEventBus() EventBus

	// GetStorage mengambil storage
	GetStorage() Storage

	// GetCrypto mengambil crypto
	GetCrypto() Crypto

	// GetScheduler mengambil scheduler
	GetScheduler() Scheduler

	// GetConfig mengambil config
	GetConfig() Config

	// ============================================================
	// 🔥 STEP ⑨: CONTAINER FINAL
	// ============================================================

	// Has mengecek apakah service terdaftar
	Has(name string) bool

	// GetDefinition mengembalikan definisi service
	GetDefinition(name string) (ServiceDefinition, bool)

	// Stats mengembalikan statistik container
	Stats() ContainerStats

	// IsReady mengecek apakah container sudah siap
	IsReady() bool

	// SetReady menandai container sebagai siap
	SetReady()

	// Validate memvalidasi semua service
	Validate() error
}

// ContainerStats adalah statistik container
type ContainerStats struct {
	TotalServices   int                        `json:"total_services"`
	SingletonCount  int                        `json:"singleton_count"`
	ModuleCount     int                        `json:"module_count"`
	RequestCount    int                        `json:"request_count"`
	TenantCount     int                        `json:"tenant_count"`
	Ready           bool                       `json:"ready"`
	ServiceNames    []string                   `json:"service_names"`
	ServiceTypes    map[string]string          `json:"service_types"`
}