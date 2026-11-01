// core/container/container.go
package container

import (
	"fmt"
	"sync"

	"trailseal/sdk"
)

// containerImpl adalah implementasi Service Container
type containerImpl struct {
	mu          sync.RWMutex
	services    map[string]interface{}
	definitions map[string]sdk.ServiceDefinition
	factories   map[string]func(sdk.Container) interface{}
	ready       bool
}

// NewContainer membuat container baru
func NewContainer() sdk.Container {
	return &containerImpl{
		services:    make(map[string]interface{}),
		definitions: make(map[string]sdk.ServiceDefinition),
		factories:   make(map[string]func(sdk.Container) interface{}),
		ready:       false,
	}
}

// ============================================================
// BASIC OPERATIONS
// ============================================================

// Get mengambil service dari container
func (c *containerImpl) Get(name string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Cek apakah ada factory untuk service ini
	if factory, ok := c.factories[name]; ok {
		// Factory-based service — buat instance
		service := factory(c)
		c.services[name] = service
		return service
	}

	return c.services[name]
}

// Register mendaftarkan service ke container
func (c *containerImpl) Register(name string, service interface{}) {
	c.RegisterWithScope(name, service, sdk.ScopeSingleton)
}

// RegisterWithScope mendaftarkan service dengan scope
func (c *containerImpl) RegisterWithScope(name string, service interface{}, scope sdk.ContainerScope) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.services[name] = service
	c.definitions[name] = sdk.ServiceDefinition{
		Name:        name,
		Service:     service,
		Scope:       scope,
		IsSingleton: scope == sdk.ScopeSingleton,
		IsReady:     true,
	}
}

// RegisterFactory mendaftarkan service dengan factory
func (c *containerImpl) RegisterFactory(name string, factory func(sdk.Container) interface{}, scope sdk.ContainerScope) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.factories[name] = factory
	c.definitions[name] = sdk.ServiceDefinition{
		Name:        name,
		Scope:       scope,
		Factory:     factory,
		IsSingleton: scope == sdk.ScopeSingleton,
		IsReady:     true,
	}
}

// ============================================================
// SERVICE RETRIEVAL (type-safe)
// ============================================================

func (c *containerImpl) GetLogger() sdk.Logger {
	if v := c.Get("logger"); v != nil {
		if logger, ok := v.(sdk.Logger); ok {
			return logger
		}
	}
	return nil
}

func (c *containerImpl) GetEventBus() sdk.EventBus {
	if v := c.Get("eventbus"); v != nil {
		if eventBus, ok := v.(sdk.EventBus); ok {
			return eventBus
		}
	}
	return nil
}

func (c *containerImpl) GetStorage() sdk.Storage {
	if v := c.Get("storage"); v != nil {
		if storage, ok := v.(sdk.Storage); ok {
			return storage
		}
	}
	return nil
}

func (c *containerImpl) GetCrypto() sdk.Crypto {
	if v := c.Get("crypto"); v != nil {
		if crypto, ok := v.(sdk.Crypto); ok {
			return crypto
		}
	}
	return nil
}

func (c *containerImpl) GetScheduler() sdk.Scheduler {
	if v := c.Get("scheduler"); v != nil {
		if scheduler, ok := v.(sdk.Scheduler); ok {
			return scheduler
		}
	}
	return nil
}

func (c *containerImpl) GetConfig() sdk.Config {
	if v := c.Get("config"); v != nil {
		if config, ok := v.(sdk.Config); ok {
			return config
		}
	}
	return nil
}

// ============================================================
// 🔥 STEP ⑨: CONTAINER FINAL
// ============================================================

// Has mengecek apakah service terdaftar
func (c *containerImpl) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.services[name]
	if !ok {
		_, ok = c.factories[name]
	}
	return ok
}

// GetDefinition mengembalikan definisi service
func (c *containerImpl) GetDefinition(name string) (sdk.ServiceDefinition, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	def, ok := c.definitions[name]
	return def, ok
}

// Stats mengembalikan statistik container
func (c *containerImpl) Stats() sdk.ContainerStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := sdk.ContainerStats{
		TotalServices: len(c.services) + len(c.factories),
		Ready:         c.ready,
		ServiceNames:  make([]string, 0),
		ServiceTypes:  make(map[string]string),
	}

	for name := range c.services {
		stats.ServiceNames = append(stats.ServiceNames, name)
		if def, ok := c.definitions[name]; ok {
			switch def.Scope {
			case sdk.ScopeSingleton:
				stats.SingletonCount++
				stats.ServiceTypes[name] = "singleton"
			case sdk.ScopeModule:
				stats.ModuleCount++
				stats.ServiceTypes[name] = "module"
			case sdk.ScopeRequest:
				stats.RequestCount++
				stats.ServiceTypes[name] = "request"
			case sdk.ScopeTenant:
				stats.TenantCount++
				stats.ServiceTypes[name] = "tenant"
			}
		}
	}

	for name := range c.factories {
		// Tambahkan factory services ke list jika belum ada
		found := false
		for _, n := range stats.ServiceNames {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			stats.ServiceNames = append(stats.ServiceNames, name)
			if def, ok := c.definitions[name]; ok {
				switch def.Scope {
				case sdk.ScopeSingleton:
					stats.SingletonCount++
					stats.ServiceTypes[name] = "singleton(factory)"
				case sdk.ScopeModule:
					stats.ModuleCount++
					stats.ServiceTypes[name] = "module(factory)"
				case sdk.ScopeRequest:
					stats.RequestCount++
					stats.ServiceTypes[name] = "request(factory)"
				case sdk.ScopeTenant:
					stats.TenantCount++
					stats.ServiceTypes[name] = "tenant(factory)"
				}
			}
		}
	}

	return stats
}

// IsReady mengecek apakah container sudah siap
func (c *containerImpl) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// SetReady menandai container sebagai siap
func (c *containerImpl) SetReady() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready = true
}

// Validate memvalidasi semua service
func (c *containerImpl) Validate() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Cek service yang terdaftar
	for name, service := range c.services {
		if service == nil {
			return fmt.Errorf("service %s is nil", name)
		}
	}

	// Cek factory service (belum di-instantiate)
	for name := range c.factories {
		if _, ok := c.services[name]; !ok {
			// Factory belum dipanggil — OK
		}
	}

	return nil
}