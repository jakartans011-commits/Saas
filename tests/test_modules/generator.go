// tests/test_modules/generator.go
package test_modules

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"trailseal/sdk"
)

// GeneratedModule adalah modul test yang digenerate secara dinamis.
type GeneratedModule struct {
	name         string
	version      string
	category     string
	capabilities []string
	permissions  []string

	logger   sdk.Logger
	eventBus sdk.EventBus
	storage  sdk.Storage

	isRunning atomic.Bool
}

// NewGeneratedModule membuat module test baru.
func NewGeneratedModule(
	name string,
	version string,
	category string,
	capabilities []string,
	permissions []string,
) *GeneratedModule {
	return &GeneratedModule{
		name:         name,
		version:      version,
		category:     category,
		capabilities: append([]string(nil), capabilities...),
		permissions:  append([]string(nil), permissions...),
	}
}

func (m *GeneratedModule) Name() string {
	return m.name
}

func (m *GeneratedModule) Version() string {
	return m.version
}

func (m *GeneratedModule) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:          m.name,
		Version:       m.version,
		SchemaVersion: "1.0.0",
		Author:        "TrailSeal Test",
		Description:   fmt.Sprintf("Generated test module for %s", m.category),
		Category:      m.category,
		Capabilities:  append([]string(nil), m.capabilities...),
		Permissions:   append([]string(nil), m.permissions...),
		Priority:      10,
	}
}

func (m *GeneratedModule) Init(config sdk.Config) error {
	return nil
}

func (m *GeneratedModule) Start(ctx context.Context) error {
	if !m.isRunning.CompareAndSwap(false, true) {
		return nil
	}

	if m.eventBus != nil {
		m.eventBus.Publish(sdk.Event{
			Type:   "module.started",
			Source: m.name,
			Data: map[string]interface{}{
				"category": m.category,
				"time":     time.Now().Unix(),
			},
		})
	}

	return nil
}

func (m *GeneratedModule) Stop() error {
	m.isRunning.Store(false)
	return nil
}

func (m *GeneratedModule) Health() error {
	if !m.isRunning.Load() {
		return fmt.Errorf("module not running")
	}

	return nil
}

// SetLogger memasang logger dari DI container.
func (m *GeneratedModule) SetLogger(logger sdk.Logger) {
	m.logger = logger
}

// SetEventBus memasang event bus dari DI container.
func (m *GeneratedModule) SetEventBus(eventBus sdk.EventBus) {
	m.eventBus = eventBus
}

// SetStorage memasang storage dari DI container.
func (m *GeneratedModule) SetStorage(storage sdk.Storage) {
	m.storage = storage
}
