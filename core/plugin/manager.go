// core/plugin/manager.go
package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	// "trailseal/core"  // ← HAPUS INI! Tidak digunakan lagi
	"trailseal/core/dependency"
	"trailseal/core/lifecycle"
	"trailseal/core/permission"
	"trailseal/sdk"
)

// Manager adalah pengelola plugin
type Manager struct {
	mu              sync.RWMutex
	plugins         map[string]*PluginInstance
	registry        sdk.Registry
	container       sdk.Container
	eventBus        sdk.EventBus
	logger          sdk.Logger
	storage         sdk.Storage
	crypto          sdk.Crypto
	scheduler       sdk.Scheduler
	config          sdk.Config
	permEngine      *permission.Engine
	depResolver     *dependency.Resolver
	loadOrder       []string
	lifecycleConfig *lifecycle.Config
	ctx             context.Context
	cancel          context.CancelFunc
	isRunning       bool
	shutdownTimeout time.Duration
}

// PluginInstance adalah instance plugin yang sedang berjalan
type PluginInstance struct {
	Name      string
	Plugin    sdk.Plugin
	Manifest  sdk.Manifest
	Lifecycle *lifecycle.Lifecycle
	Status    Status
	StartedAt time.Time
	StoppedAt time.Time
	Error     error
	mu        sync.RWMutex
}

// Status adalah status plugin
type Status string

const (
	StatusRegistered Status = "registered"
	StatusLoading    Status = "loading"
	StatusLoaded     Status = "loaded"
	StatusValidating Status = "validating"
	StatusValid      Status = "valid"
	StatusInvalid    Status = "invalid"
	StatusStarting   Status = "starting"
	StatusRunning    Status = "running"
	StatusStopping   Status = "stopping"
	StatusStopped    Status = "stopped"
	StatusError      Status = "error"
)

// NewManager membuat plugin manager baru
func NewManager(
	registry sdk.Registry,
	container sdk.Container,
	eventBus sdk.EventBus,
	logger sdk.Logger,
	storage sdk.Storage,
	crypto sdk.Crypto,
	scheduler sdk.Scheduler,
	config sdk.Config,
) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		plugins:     make(map[string]*PluginInstance),
		registry:    registry,
		container:   container,
		eventBus:    eventBus,
		logger:      logger,
		storage:     storage,
		crypto:      crypto,
		scheduler:   scheduler,
		config:      config,
		permEngine:  permission.NewEngine(),
		depResolver: dependency.NewResolver(),
		loadOrder:   []string{},
		lifecycleConfig: &lifecycle.Config{
			MaxAttempts: 3,
			Timeout:     30 * time.Second,
		},
		ctx:             ctx,
		cancel:          cancel,
		isRunning:       false,
		shutdownTimeout: 30 * time.Second,
	}
}

// SetLifecycleConfig mengatur konfigurasi lifecycle
func (m *Manager) SetLifecycleConfig(cfg *lifecycle.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lifecycleConfig = cfg
}

// SetShutdownTimeout mengatur timeout shutdown
func (m *Manager) SetShutdownTimeout(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdownTimeout = timeout
}

// GetPermissionEngine mengembalikan permission engine
func (m *Manager) GetPermissionEngine() *permission.Engine {
	return m.permEngine
}

// GetDependencyResolver mengembalikan dependency resolver
func (m *Manager) GetDependencyResolver() *dependency.Resolver {
	return m.depResolver
}

// GetScheduler mengembalikan scheduler
func (m *Manager) GetScheduler() sdk.Scheduler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scheduler
}

// ============================================================
// 🔥 SDK.PLUGINMANAGER IMPLEMENTATION
// ============================================================

// LoadAll memuat semua plugin dari registry dengan dependency order
// 🔥 FIX: Pakai m.registry, BUKAN core global!
func (m *Manager) LoadAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Loading all plugins...")

	// 🔥 FIX: Ambil dari m.registry, BUKAN core.ListManifests()!
	allManifests := m.registry.ListManifests()
	if len(allManifests) == 0 {
		m.logger.Info("No plugins registered")
		return nil
	}

	if err := m.depResolver.BuildGraph(allManifests); err != nil {
		return fmt.Errorf("dependency graph build failed: %w", err)
	}

	order, err := m.depResolver.ResolveOrder()
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}
	m.loadOrder = order

	m.logger.Info("Load order resolved",
		"order", order,
		"total", len(order),
	)

	for _, name := range order {
		// 🔥 FIX: Ambil dari m.registry, BUKAN core.GetPlugin()!
		factory, ok := m.registry.Get(name)
		if !ok {
			m.logger.Warn("Plugin factory not found", "name", name)
			continue
		}

		// 🔥 FIX: Ambil dari m.registry, BUKAN core.GetManifest()!
		manifest, ok := m.registry.GetManifest(name)
		if !ok {
			m.logger.Warn("Plugin manifest not found", "name", name)
			continue
		}

		plugin := factory(m.container)

		lifecycleInstance := lifecycle.NewLifecycle(
			plugin,
			m.logger,
			m.eventBus,
			m.config,
		)

		instance := &PluginInstance{
			Name:      name,
			Plugin:    plugin,
			Manifest:  manifest,
			Lifecycle: lifecycleInstance,
			Status:    StatusLoaded,
		}

		m.plugins[name] = instance
		m.logger.Info("Plugin loaded",
			"name", name,
			"version", manifest.Version,
			"category", manifest.Category,
			"state", lifecycleInstance.State(),
		)
	}

	m.isRunning = true
	return nil
}

// ValidateAll memvalidasi semua plugin
func (m *Manager) ValidateAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Validating all plugins...")

	for _, name := range m.loadOrder {
		instance, ok := m.plugins[name]
		if !ok {
			continue
		}

		if err := m.validatePlugin(instance); err != nil {
			instance.Status = StatusInvalid
			instance.Error = err
			m.logger.Warn("Plugin validation failed", "name", name, "error", err)
			m.eventBus.Publish(sdk.Event{
				Type:   "plugin.validation_failed",
				Source: name,
				Data: map[string]interface{}{
					"error": err.Error(),
					"time":  time.Now().Unix(),
				},
			})
			continue
		}
		instance.Status = StatusValid
	}

	return nil
}

// validatePlugin memvalidasi satu plugin
func (m *Manager) validatePlugin(instance *PluginInstance) error {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	manifest := instance.Manifest

	if manifest.Category == "" {
		m.logger.Warn("Plugin has no category", "name", manifest.Name)
	}

	if len(manifest.Capabilities) == 0 {
		m.logger.Warn("Plugin has no capabilities", "name", manifest.Name)
	}

	if len(manifest.Requirements) == 0 {
		m.logger.Warn("Plugin has no requirements", "name", manifest.Name)
	}

	if err := m.permEngine.ValidateManifestPermissions(manifest); err != nil {
		return fmt.Errorf("permission validation failed: %w", err)
	}

	if err := instance.Lifecycle.Health(); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// StartAll memulai semua plugin (dalam urutan dependency)
func (m *Manager) StartAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Starting all plugins...")

	for _, name := range m.loadOrder {
		instance, ok := m.plugins[name]
		if !ok {
			continue
		}

		if err := m.startPlugin(instance); err != nil {
			instance.Status = StatusError
			instance.Error = err
			m.logger.Error("Plugin start failed", "name", name, "error", err)
			return fmt.Errorf("plugin %s start failed: %w", name, err)
		}
	}

	return nil
}

// startPlugin memulai satu plugin via lifecycle
func (m *Manager) startPlugin(instance *PluginInstance) error {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.Status == StatusRunning {
		return nil
	}

	if err := instance.Lifecycle.Init(m.ctx); err != nil {
		return fmt.Errorf("lifecycle init failed: %w", err)
	}

	if err := instance.Lifecycle.Start(m.ctx); err != nil {
		return fmt.Errorf("lifecycle start failed: %w", err)
	}

	instance.Status = StatusRunning
	instance.StartedAt = time.Now()

	m.eventBus.Publish(sdk.Event{
		Type:   "plugin.started",
		Source: instance.Name,
		Data: map[string]interface{}{
			"version":  instance.Manifest.Version,
			"category": instance.Manifest.Category,
			"state":    instance.Lifecycle.State().String(),
			"time":     time.Now().Unix(),
		},
	})

	m.logger.Info("Plugin started",
		"name", instance.Name,
		"category", instance.Manifest.Category,
		"state", instance.Lifecycle.State(),
	)

	return nil
}

// StopAll menghentikan semua plugin (urutan terbalik) dengan graceful shutdown
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Stopping all plugins...")

	ctx, cancel := context.WithTimeout(context.Background(), m.shutdownTimeout)
	defer cancel()

	for i := len(m.loadOrder) - 1; i >= 0; i-- {
		name := m.loadOrder[i]
		instance, ok := m.plugins[name]
		if !ok {
			continue
		}

		if err := m.stopPlugin(ctx, instance); err != nil {
			m.logger.Warn("Plugin stop error", "name", name, "error", err)
		}
	}

	m.cancel()
	return nil
}

// stopPlugin menghentikan satu plugin via lifecycle
func (m *Manager) stopPlugin(ctx context.Context, instance *PluginInstance) error {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.Status != StatusRunning {
		return nil
	}

	if err := instance.Lifecycle.Stop(ctx); err != nil {
		instance.Status = StatusError
		instance.Error = err
		return fmt.Errorf("lifecycle stop failed: %w", err)
	}

	instance.Status = StatusStopped
	instance.StoppedAt = time.Now()

	m.eventBus.Publish(sdk.Event{
		Type:   "plugin.stopped",
		Source: instance.Name,
		Data: map[string]interface{}{
			"state": instance.Lifecycle.State().String(),
			"time":  time.Now().Unix(),
		},
	})

	m.logger.Info("Plugin stopped",
		"name", instance.Name,
		"state", instance.Lifecycle.State(),
	)

	return nil
}

// HealthCheck memeriksa kesehatan semua plugin
func (m *Manager) HealthCheck() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]error)
	for name, instance := range m.plugins {
		if err := instance.Lifecycle.Health(); err != nil {
			results[name] = err
			m.logger.Warn("Plugin health check failed", "name", name, "error", err)
		}
	}
	return results
}

// GetPlugin mengembalikan instance plugin sebagai interface{}
func (m *Manager) GetPlugin(name string) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instance, ok := m.plugins[name]
	if !ok {
		return nil, false
	}
	return instance, true
}

// GetPluginInstance mengembalikan instance plugin (internal)
func (m *Manager) GetPluginInstance(name string) (*PluginInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instance, ok := m.plugins[name]
	return instance, ok
}

// GetPluginState mengembalikan state plugin
func (m *Manager) GetPluginState(name string) (lifecycle.State, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instance, ok := m.plugins[name]
	if !ok {
		return lifecycle.StateError, false
	}
	return instance.Lifecycle.State(), true
}

// ListPlugins mengembalikan semua nama plugin
func (m *Manager) ListPlugins() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// ListRunning mengembalikan plugin yang sedang berjalan
func (m *Manager) ListRunning() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0)
	for name, instance := range m.plugins {
		if instance.Status == StatusRunning {
			names = append(names, name)
		}
	}
	return names
}

// GetLoadOrder mengembalikan urutan load plugin (dependency order)
func (m *Manager) GetLoadOrder() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadOrder
}

// GetPluginInfo mengembalikan informasi lengkap plugin
func (m *Manager) GetPluginInfo(name string) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instance, ok := m.plugins[name]
	if !ok {
		return nil
	}

	info := map[string]interface{}{
		"name":     instance.Name,
		"version":  instance.Manifest.Version,
		"category": instance.Manifest.Category,
		"status":   instance.Status,
		"state":    instance.Lifecycle.State().String(),
	}

	if !instance.StartedAt.IsZero() {
		info["started_at"] = instance.StartedAt
	}
	if !instance.StoppedAt.IsZero() {
		info["stopped_at"] = instance.StoppedAt
	}
	if instance.Error != nil {
		info["error"] = instance.Error.Error()
	}

	return info
}

// EnablePlugin mengaktifkan plugin
func (m *Manager) EnablePlugin(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %s not found", name)
	}

	if instance.Status == StatusRunning {
		return nil
	}

	return m.startPlugin(instance)
}

// DisablePlugin menonaktifkan plugin
func (m *Manager) DisablePlugin(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %s not found", name)
	}

	if instance.Status != StatusRunning {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), m.shutdownTimeout)
	defer cancel()

	return m.stopPlugin(ctx, instance)
}

// IsEnabled memeriksa apakah plugin aktif
func (m *Manager) IsEnabled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instance, ok := m.plugins[name]
	if !ok {
		return false
	}
	return instance.Status == StatusRunning
}

// RegisterExecutable mendaftarkan executable ke scheduler
func (m *Manager) RegisterExecutable(executable sdk.Executable) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return fmt.Errorf("plugin manager not running")
	}

	if m.scheduler == nil {
		return fmt.Errorf("scheduler not available")
	}

	return m.scheduler.Register(executable)
}

// UnregisterExecutable menghapus executable dari scheduler
func (m *Manager) UnregisterExecutable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return fmt.Errorf("plugin manager not running")
	}

	if m.scheduler == nil {
		return fmt.Errorf("scheduler not available")
	}

	return m.scheduler.Unregister(name)
}

// TriggerExecutable secara manual menjalankan executable
func (m *Manager) TriggerExecutable(name string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.isRunning {
		return fmt.Errorf("plugin manager not running")
	}

	if m.scheduler == nil {
		return fmt.Errorf("scheduler not available")
	}

	return m.scheduler.Trigger(name)
}
