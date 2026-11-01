// core/module.go
package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ============================================================================
// MODULE STATE
// ============================================================================

type ModuleState string

const (
	ModuleStateRegistered ModuleState = "registered"
	ModuleStateInitialized ModuleState = "initialized"
	ModuleStateStarting   ModuleState = "starting"
	ModuleStateRunning    ModuleState = "running"
	ModuleStateStopping   ModuleState = "stopping"
	ModuleStateStopped    ModuleState = "stopped"
	ModuleStateError      ModuleState = "error"
	ModuleStateReloading  ModuleState = "reloading"
)

func (s ModuleState) String() string { return string(s) }

// ============================================================================
// MODULE VERSION
// ============================================================================

type ModuleVersion struct {
	Major      int    `json:"major"`
	Minor      int    `json:"minor"`
	Patch      int    `json:"patch"`
	Build      string `json:"build,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Compatible bool   `json:"compatible"`
}

func (v ModuleVersion) String() string {
	if v.Build != "" {
		return fmt.Sprintf("v%d.%d.%d-%s", v.Major, v.Minor, v.Patch, v.Build)
	}
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ============================================================================
// MODULE METADATA
// ============================================================================

type ModuleMetadata struct {
	Name         string         `json:"name"`
	Version      ModuleVersion  `json:"version"`
	Description  string         `json:"description"`
	Author       string         `json:"author"`
	Website      string         `json:"website,omitempty"`
	License      string         `json:"license,omitempty"`
	Priority     int            `json:"priority"`
	Permissions  []string       `json:"permissions"`
	Dependencies []string       `json:"dependencies"`
	Tags         []string       `json:"tags,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// ============================================================================
// MODULE HEALTH
// ============================================================================

type ModuleHealth struct {
	State        ModuleState   `json:"state"`
	Status       string        `json:"status"`
	Score        int           `json:"score"`
	LastCheck    time.Time     `json:"last_check"`
	ErrorMessage string        `json:"error_message,omitempty"`
	Uptime       time.Duration `json:"uptime"`
	MemoryUsage  uint64        `json:"memory_usage"`
	CPUUsage     float64       `json:"cpu_usage"`
}

// ============================================================================
// MODULE INTERFACE
// ============================================================================

type Module interface {
	Name() string
	Version() ModuleVersion
	Metadata() ModuleMetadata
	Init(config map[string]interface{}) error
	Validate() error
	Start(ctx context.Context) error
	Stop() error
	HealthCheck(ctx context.Context) error
	Dependencies() []string
	Priority() int
	CanReload() bool
	Reload(ctx context.Context, config map[string]interface{}) error
	GetState() ModuleState
	SetState(state ModuleState)
	GetHealth() ModuleHealth
}

// ============================================================================
// BASE MODULE
// ============================================================================

type BaseModule struct {
	name       string
	version    ModuleVersion
	metadata   ModuleMetadata
	state      ModuleState
	mu         sync.RWMutex
	startedAt  time.Time
	health     ModuleHealth
}

func NewBaseModule(name string, version ModuleVersion, metadata ModuleMetadata) *BaseModule {
	return &BaseModule{
		name:     name,
		version:  version,
		metadata: metadata,
		state:    ModuleStateRegistered,
		health: ModuleHealth{
			State:  ModuleStateRegistered,
			Status: "unknown",
			Score:  0,
		},
	}
}

func (m *BaseModule) Name() string                    { return m.name }
func (m *BaseModule) Version() ModuleVersion          { return m.version }
func (m *BaseModule) Metadata() ModuleMetadata        { return m.metadata }
func (m *BaseModule) Priority() int                   { return m.metadata.Priority }

func (m *BaseModule) Dependencies() []string {
	return m.metadata.Dependencies
}

func (m *BaseModule) GetState() ModuleState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *BaseModule) SetState(state ModuleState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
}

func (m *BaseModule) GetHealth() ModuleHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.health
}

func (m *BaseModule) CanReload() bool {
	return true
}

func (m *BaseModule) Reload(ctx context.Context, config map[string]interface{}) error {
	if err := m.Stop(); err != nil {
		return err
	}
	if err := m.Init(config); err != nil {
		return err
	}
	return m.Start(ctx)
}

func (m *BaseModule) Init(config map[string]interface{}) error {
	m.SetState(ModuleStateInitialized)
	return nil
}

func (m *BaseModule) Validate() error {
	return nil
}

func (m *BaseModule) Start(ctx context.Context) error {
	m.mu.Lock()
	m.startedAt = time.Now()
	m.mu.Unlock()
	m.SetState(ModuleStateRunning)
	return nil
}

func (m *BaseModule) Stop() error {
	m.SetState(ModuleStateStopped)
	return nil
}

func (m *BaseModule) HealthCheck(ctx context.Context) error {
	return nil
}

// ============================================================================
// MODULE REGISTRY
// ============================================================================

type ModuleRegistry struct {
	mu       sync.RWMutex
	modules  map[string]RegisteredModule
	eventBus *EventBus
}

type RegisteredModule struct {
	Module       Module
	RegisteredAt time.Time
	Enabled      bool
	LastError    error
	StartCount   int
	StopCount    int
}

func NewModuleRegistry(eventBus *EventBus) *ModuleRegistry {
	return &ModuleRegistry{
		modules:  make(map[string]RegisteredModule),
		eventBus: eventBus,
	}
}

func (r *ModuleRegistry) Register(module Module) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := module.Name()
	if _, exists := r.modules[name]; exists {
		return fmt.Errorf("module %s already registered", name)
	}

	for _, dep := range module.Dependencies() {
		if _, exists := r.modules[dep]; !exists {
			return fmt.Errorf("dependency %s not found for module %s", dep, name)
		}
	}

	r.modules[name] = RegisteredModule{
		Module:       module,
		RegisteredAt: time.Now(),
		Enabled:      true,
		StartCount:   0,
		StopCount:    0,
	}

	if r.eventBus != nil {
		r.eventBus.Publish(context.Background(), Event{
			Type:   "module.registered",
			Source: "registry",
			Data: map[string]interface{}{
				"name":    name,
				"version": module.Version().String(),
			},
		})
	}

	return nil
}

func (r *ModuleRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	reg, exists := r.modules[name]
	if !exists {
		return fmt.Errorf("module %s not found", name)
	}

	if err := reg.Module.Stop(); err != nil {
		return fmt.Errorf("failed to stop module %s: %w", name, err)
	}

	delete(r.modules, name)

	if r.eventBus != nil {
		r.eventBus.Publish(context.Background(), Event{
			Type:   "module.unregistered",
			Source: "registry",
			Data:   map[string]interface{}{"name": name},
		})
	}

	return nil
}

func (r *ModuleRegistry) Get(name string) (Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	reg, exists := r.modules[name]
	if !exists {
		return nil, false
	}
	return reg.Module, true
}

func (r *ModuleRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	return names
}

func (r *ModuleRegistry) ListWithMetadata() []ModuleMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metas := make([]ModuleMetadata, 0, len(r.modules))
	for _, reg := range r.modules {
		metas = append(metas, reg.Module.Metadata())
	}
	return metas
}

func (r *ModuleRegistry) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := len(r.modules)
	enabled := 0
	healthy := 0

	for _, reg := range r.modules {
		if reg.Enabled {
			enabled++
		}
		if reg.Module.GetState() == ModuleStateRunning {
			healthy++
		}
	}

	return map[string]interface{}{
		"total_modules": total,
		"enabled":       enabled,
		"healthy":       healthy,
		"modules":       r.List(),
	}
}

// ============================================================================
// MODULE MANAGER
// ============================================================================

type ModuleManager struct {
	registry *ModuleRegistry
	eventBus *EventBus
	logger   *slog.Logger
}

func NewModuleManager(registry *ModuleRegistry, eventBus *EventBus, logger *slog.Logger) *ModuleManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &ModuleManager{
		registry: registry,
		eventBus: eventBus,
		logger:   logger,
	}
}

func (m *ModuleManager) Register(module Module) error {
	return m.registry.Register(module)
}

func (m *ModuleManager) Get(name string) (Module, bool) {
	return m.registry.Get(name)
}

func (m *ModuleManager) List() []string {
	return m.registry.List()
}

func (m *ModuleManager) StartModule(name string) error {
	module, exists := m.registry.Get(name)
	if !exists {
		return fmt.Errorf("module %s not found", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := module.Start(ctx); err != nil {
		return fmt.Errorf("module %s start failed: %w", name, err)
	}

	m.logger.Info("Module started", "module", name)
	return nil
}

func (m *ModuleManager) StopModule(name string) error {
	module, exists := m.registry.Get(name)
	if !exists {
		return fmt.Errorf("module %s not found", name)
	}

	if err := module.Stop(); err != nil {
		return fmt.Errorf("module %s stop failed: %w", name, err)
	}

	m.logger.Info("Module stopped", "module", name)
	return nil
}

func (m *ModuleManager) ReloadModule(name string, config map[string]interface{}) error {
	module, exists := m.registry.Get(name)
	if !exists {
		return fmt.Errorf("module %s not found", name)
	}

	if !module.CanReload() {
		return fmt.Errorf("module %s does not support reload", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := module.Reload(ctx, config); err != nil {
		return fmt.Errorf("module %s reload failed: %w", name, err)
	}

	m.logger.Info("Module reloaded", "module", name)
	return nil
}