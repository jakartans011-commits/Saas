// core/bootstrap/test_modules/stateful/stateful.go
package stateful

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"trailseal/sdk"
)

// StatefulModule adalah test module yang memverifikasi bahwa module
// dapat mempertahankan state dan mendaftarkan executable ke scheduler.
type StatefulModule struct {
	name      string
	scheduler sdk.Scheduler
	storage   sdk.Storage

	state map[string]interface{}
	mu    sync.RWMutex

	running atomic.Bool

	executable *StatefulExecutable
}

// StatefulExecutable adalah executable yang dijalankan oleh scheduler.
//
// Implementasi ini mengikuti kontrak SDK frozen:
//   - Name()
//   - Description()
//   - Execute(sdk.ExecutableContext)
//   - Cancel()
//   - Status()
type StatefulExecutable struct {
	name        string
	description string

	module *StatefulModule

	mu     sync.RWMutex
	status sdk.ExecutableStatus

	cancelMu sync.Mutex
	cancelCh chan struct{}
}

// NewStatefulModule membuat StatefulModule baru.
func NewStatefulModule(
	name string,
	scheduler sdk.Scheduler,
	storage sdk.Storage,
) *StatefulModule {
	return &StatefulModule{
		name:      name,
		scheduler: scheduler,
		storage:   storage,
		state:     make(map[string]interface{}),
	}
}

// Name mengembalikan nama module.
func (m *StatefulModule) Name() string {
	return m.name
}

// Version mengembalikan versi module.
func (m *StatefulModule) Version() string {
	return "1.0.0"
}

// Manifest mengembalikan manifest module.
func (m *StatefulModule) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:          m.name,
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		Category:      "stateful",
		Capabilities: []string{
			"state.read",
			"state.write",
			"state.schedule",
		},
		Permissions: []string{
			"storage",
			"scheduler",
		},
		Priority: 10,
	}
}

// Init melakukan inisialisasi state.
func (m *StatefulModule) Init(config sdk.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = make(map[string]interface{})
	}

	return nil
}

// Start mendaftarkan executable ke scheduler.
func (m *StatefulModule) Start(ctx context.Context) error {
	if !m.running.CompareAndSwap(false, true) {
		return nil
	}

	if m.scheduler == nil {
		m.running.Store(false)
		return fmt.Errorf("stateful module %q: scheduler is nil", m.name)
	}

	if m.storage == nil {
		m.running.Store(false)
		return fmt.Errorf("stateful module %q: storage is nil", m.name)
	}

	executable := NewStatefulExecutable(
		m.name+"-cron",
		"Persists stateful module state.",
		m,
	)

	if err := m.scheduler.Register(executable); err != nil {
		m.running.Store(false)
		return fmt.Errorf(
			"stateful module %q: register executable: %w",
			m.name,
			err,
		)
	}

	m.mu.Lock()
	m.executable = executable
	m.mu.Unlock()

	return nil
}

// Stop unregisters the executable dari scheduler.
func (m *StatefulModule) Stop() error {
	if !m.running.CompareAndSwap(true, false) {
		return nil
	}

	m.mu.Lock()
	executable := m.executable
	m.executable = nil
	m.mu.Unlock()

	if executable == nil || m.scheduler == nil {
		return nil
	}

	if err := m.scheduler.Unregister(executable.Name()); err != nil {
		return fmt.Errorf(
			"stateful module %q: unregister executable: %w",
			m.name,
			err,
		)
	}

	return nil
}

// Health mengembalikan status kesehatan module.
func (m *StatefulModule) Health() error {
	if !m.running.Load() {
		return fmt.Errorf("module not running")
	}

	return nil
}

// State mengembalikan salinan state module.
//
// Salinan digunakan supaya caller tidak dapat memodifikasi map internal
// tanpa melalui mutex.
func (m *StatefulModule) State() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]interface{}, len(m.state))

	for key, value := range m.state {
		result[key] = value
	}

	return result
}

// SetState mengubah state module secara aman.
func (m *StatefulModule) SetState(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		m.state = make(map[string]interface{})
	}

	m.state[key] = value
}

// NewStatefulExecutable membuat executable stateful.
func NewStatefulExecutable(
	name string,
	description string,
	module *StatefulModule,
) *StatefulExecutable {
	return &StatefulExecutable{
		name:        name,
		description: description,
		module:      module,
		status:      sdk.StatusPending,
		cancelCh:    make(chan struct{}),
	}
}

// Name mengimplementasikan sdk.Executable.
func (e *StatefulExecutable) Name() string {
	return e.name
}

// Description mengimplementasikan sdk.Executable.
func (e *StatefulExecutable) Description() string {
	return e.description
}

// Execute mengimplementasikan sdk.Executable.
//
// Scheduler memberikan sdk.ExecutableContext, bukan context.Context.
func (e *StatefulExecutable) Execute(
	ctx sdk.ExecutableContext,
) error {
	if ctx == nil {
		e.setStatus(sdk.StatusFailed)
		return fmt.Errorf("stateful executable %q: nil executable context", e.name)
	}

	if e.module == nil {
		e.setStatus(sdk.StatusFailed)
		return fmt.Errorf("stateful executable %q: nil module", e.name)
	}

	if e.module.storage == nil {
		e.setStatus(sdk.StatusFailed)
		return fmt.Errorf("stateful executable %q: nil storage", e.name)
	}

	if ctx.IsCancelled() {
		e.setStatus(sdk.StatusCancelled)
		return nil
	}

	select {
	case <-e.cancelCh:
		e.setStatus(sdk.StatusCancelled)
		return nil
	default:
	}

	e.setStatus(sdk.StatusRunning)

	now := time.Now().Unix()

	e.module.mu.Lock()

	if e.module.state == nil {
		e.module.state = make(map[string]interface{})
	}

	e.module.state["last_run"] = now

	// Buat snapshot supaya storage tidak menerima map internal
	// yang masih dimodifikasi setelah mutex dilepas.
	stateSnapshot := make(map[string]interface{}, len(e.module.state))

	for key, value := range e.module.state {
		stateSnapshot[key] = value
	}

	e.module.mu.Unlock()

	if ctx.IsCancelled() {
		e.setStatus(sdk.StatusCancelled)
		return nil
	}

	if deadline := ctx.Deadline(); !deadline.IsZero() &&
		time.Now().After(deadline) {
		e.setStatus(sdk.StatusTimeout)
		return fmt.Errorf("stateful executable %q: deadline exceeded", e.name)
	}

	if err := e.module.storage.Save(
		"stateful/"+e.module.name,
		stateSnapshot,
	); err != nil {
		e.setStatus(sdk.StatusFailed)

		return fmt.Errorf(
			"stateful executable %q: save state: %w",
			e.name,
			err,
		)
	}

	e.setStatus(sdk.StatusCompleted)

	return nil
}

// Cancel mengimplementasikan sdk.Executable.
func (e *StatefulExecutable) Cancel() error {
	e.cancelMu.Lock()
	defer e.cancelMu.Unlock()

	select {
	case <-e.cancelCh:
		e.setStatus(sdk.StatusCancelled)
		return nil

	default:
		close(e.cancelCh)
		e.setStatus(sdk.StatusCancelled)
		return nil
	}
}

// Status mengimplementasikan sdk.Executable.
func (e *StatefulExecutable) Status() sdk.ExecutableStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.status
}

// setStatus mengubah status executable secara aman.
func (e *StatefulExecutable) setStatus(
	status sdk.ExecutableStatus,
) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.status = status
}
