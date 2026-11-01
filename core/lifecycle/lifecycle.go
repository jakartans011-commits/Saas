// core/lifecycle/lifecycle.go
package lifecycle

import (
	"context"
	"fmt"
	"sync"
	"time"

	"trailseal/sdk"
)

// State adalah state plugin.
type State string

const (
	StateCreated  State = "created"
	StateInit     State = "init"
	StateValid    State = "valid"
	StateInvalid  State = "invalid"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StatePausing  State = "pausing"
	StatePaused   State = "paused"
	StateResuming State = "resuming"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateDestroy  State = "destroy"
	StateError    State = "error"
)

// String returns string representation of State.
func (s State) String() string {
	return string(s)
}

// IsActive returns true if state is active (running or paused).
func (s State) IsActive() bool {
	return s == StateRunning || s == StatePaused
}

// IsTerminal returns true if state is terminal (stopped, destroy, error).
func (s State) IsTerminal() bool {
	return s == StateStopped || s == StateDestroy || s == StateError
}

// Lifecycle adalah state machine untuk plugin.
// Semua plugin WAJIB mengikuti state machine yang sama!
type Lifecycle struct {
	mu          sync.RWMutex
	plugin      sdk.Plugin
	state       State
	prevState   State
	startedAt   time.Time
	stoppedAt   time.Time
	lastError   error
	attempts    int
	maxAttempts int
	timeout     time.Duration
	logger      sdk.Logger
	eventBus    sdk.EventBus
	config      sdk.Config
}

// Config adalah konfigurasi lifecycle.
type Config struct {
	MaxAttempts int
	Timeout     time.Duration
}

// DefaultConfig returns default lifecycle config.
func DefaultConfig() *Config {
	return &Config{
		MaxAttempts: 3,
		Timeout:     30 * time.Second,
	}
}

// NewLifecycle membuat lifecycle baru untuk plugin.
func NewLifecycle(
	plugin sdk.Plugin,
	logger sdk.Logger,
	eventBus sdk.EventBus,
	config sdk.Config,
) *Lifecycle {
	cfg := DefaultConfig()

	if config != nil {
		if v := config.GetInt("max_attempts"); v > 0 {
			cfg.MaxAttempts = v
		}

		if v := config.GetDuration("timeout"); v > 0 {
			cfg.Timeout = v
		}
	}

	return &Lifecycle{
		plugin:      plugin,
		state:       StateCreated,
		logger:      logger,
		eventBus:    eventBus,
		config:      config,
		maxAttempts: cfg.MaxAttempts,
		timeout:     cfg.Timeout,
	}
}

// State returns current state.
func (l *Lifecycle) State() State {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.state
}

// IsRunning returns true if plugin is running.
func (l *Lifecycle) IsRunning() bool {
	return l.State() == StateRunning
}

// IsActive returns true if plugin is active.
func (l *Lifecycle) IsActive() bool {
	return l.State().IsActive()
}

// Init initializes the plugin.
func (l *Lifecycle) Init(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StateCreated {
		return fmt.Errorf("cannot init from state %s", l.state)
	}

	// Respect an already-cancelled context before starting initialization.
	if err := ctx.Err(); err != nil {
		return err
	}

	l.setState(StateInit)
	l.logger.Info("Initializing plugin", "name", l.plugin.Name())

	if err := l.plugin.Init(l.config); err != nil {
		l.setState(StateError)
		l.lastError = err
		return fmt.Errorf("init failed: %w", err)
	}

	l.setState(StateValid)
	l.logger.Info("Plugin initialized", "name", l.plugin.Name())

	l.publishEvent("plugin.init")
	return nil
}

// Validate validates the plugin.
func (l *Lifecycle) Validate(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StateValid && l.state != StateInit {
		return fmt.Errorf("cannot validate from state %s", l.state)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	l.logger.Info("Validating plugin", "name", l.plugin.Name())

	if err := l.plugin.Health(); err != nil {
		l.setState(StateInvalid)
		l.lastError = err
		return fmt.Errorf("validation failed: %w", err)
	}

	l.setState(StateValid)
	l.logger.Info("Plugin validated", "name", l.plugin.Name())

	l.publishEvent("plugin.validated")
	return nil
}

// Start starts the plugin.
func (l *Lifecycle) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StateValid && l.state != StateStopped {
		return fmt.Errorf("cannot start from state %s", l.state)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	l.setState(StateStarting)
	l.attempts = 0
	l.lastError = nil

	l.logger.Info("Starting plugin", "name", l.plugin.Name())

	// Try to start with retry.
	for {
		l.attempts++

		// Create a fresh timeout context for each attempt.
		startCtx, cancel := context.WithTimeout(ctx, l.timeout)

		err := l.plugin.Start(startCtx)

		// Do not defer cancel inside the retry loop.
		cancel()

		if err == nil {
			break
		}

		l.lastError = err

		l.logger.Warn(
			"Start attempt failed",
			"name", l.plugin.Name(),
			"attempt", l.attempts,
			"max", l.maxAttempts,
			"error", err,
		)

		if l.attempts >= l.maxAttempts {
			l.setState(StateError)

			return fmt.Errorf(
				"start failed after %d attempts: %w",
				l.attempts,
				err,
			)
		}

		// If the caller cancelled the parent context, do not retry.
		if err := ctx.Err(); err != nil {
			return err
		}

		// Wait before retry, while still respecting caller cancellation.
		timer := time.NewTimer(2 * time.Second)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			return ctx.Err()

		case <-timer.C:
			continue
		}
	}

	l.setState(StateRunning)
	l.startedAt = time.Now()
	l.stoppedAt = time.Time{}
	l.lastError = nil

	l.logger.Info("Plugin started", "name", l.plugin.Name())

	l.publishEvent("plugin.started")
	return nil
}

// Pause pauses the plugin.
func (l *Lifecycle) Pause(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StateRunning {
		return fmt.Errorf("cannot pause from state %s", l.state)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	l.setState(StatePausing)
	l.logger.Info("Pausing plugin", "name", l.plugin.Name())

	// Pause implementation (if plugin supports it).
	// Current lifecycle contract keeps the existing behavior:
	// transition to paused without invoking plugin.Stop().
	l.setState(StatePaused)

	l.logger.Info("Plugin paused", "name", l.plugin.Name())

	l.publishEvent("plugin.paused")
	return nil
}

// Resume resumes the plugin.
func (l *Lifecycle) Resume(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StatePaused {
		return fmt.Errorf("cannot resume from state %s", l.state)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	l.setState(StateResuming)
	l.logger.Info("Resuming plugin", "name", l.plugin.Name())

	if err := l.plugin.Start(ctx); err != nil {
		l.setState(StateError)
		l.lastError = err
		return fmt.Errorf("resume failed: %w", err)
	}

	l.setState(StateRunning)
	l.lastError = nil

	l.logger.Info("Plugin resumed", "name", l.plugin.Name())

	l.publishEvent("plugin.resumed")
	return nil
}

// Stop stops the plugin.
func (l *Lifecycle) Stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StateRunning &&
		l.state != StatePaused &&
		l.state != StateError {
		return fmt.Errorf("cannot stop from state %s", l.state)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	l.setState(StateStopping)
	l.logger.Info("Stopping plugin", "name", l.plugin.Name())

	stopCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	errChan := make(chan error, 1)

	go func() {
		errChan <- l.plugin.Stop()
	}()

	select {
	case err := <-errChan:
		if err != nil {
			l.setState(StateError)
			l.lastError = err

			return fmt.Errorf("stop failed: %w", err)
		}

	case <-stopCtx.Done():
		l.setState(StateError)
		l.lastError = stopCtx.Err()

		return fmt.Errorf("stop timeout: %w", stopCtx.Err())
	}

	l.setState(StateStopped)
	l.stoppedAt = time.Now()

	l.logger.Info("Plugin stopped", "name", l.plugin.Name())

	l.publishEvent("plugin.stopped")
	return nil
}

// Destroy destroys the plugin.
func (l *Lifecycle) Destroy(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StateStopped &&
		l.state != StateError &&
		l.state != StateInvalid {
		return fmt.Errorf("cannot destroy from state %s", l.state)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	l.setState(StateDestroy)
	l.logger.Info("Destroying plugin", "name", l.plugin.Name())

	// The plugin has already been stopped when entering StateStopped.
	// Do not call plugin.Stop() again here.
	//
	// For StateError/StateInvalid, the existing contract is also kept
	// as cleanup-only lifecycle transition. We intentionally avoid a
	// second Stop() call because Stop() may not be idempotent.

	l.logger.Info("Plugin destroyed", "name", l.plugin.Name())

	l.publishEvent("plugin.destroyed")
	return nil
}

// Health checks plugin health.
func (l *Lifecycle) Health() error {
	// IMPORTANT:
	// Do not hold lifecycle locks while calling plugin.Health().
	// A plugin implementation may block, perform I/O, or call back
	// into lifecycle-related code.
	l.mu.RLock()

	state := l.state

	if state != StateRunning && state != StatePaused {
		l.mu.RUnlock()

		return fmt.Errorf(
			"plugin not active, state: %s",
			state,
		)
	}

	plugin := l.plugin

	l.mu.RUnlock()

	// Call the plugin outside the lifecycle lock.
	err := plugin.Health()
	if err == nil {
		return nil
	}

	// Re-acquire the write lock only to record the failure.
	l.mu.Lock()
	defer l.mu.Unlock()

	l.setState(StateError)
	l.lastError = err

	return err
}

// setState sets the state and logs transition.
func (l *Lifecycle) setState(state State) {
	l.prevState = l.state
	l.state = state

	l.logger.Debug(
		"State transition",
		"name", l.plugin.Name(),
		"from", l.prevState,
		"to", state,
	)
}

// publishEvent publishes lifecycle event.
func (l *Lifecycle) publishEvent(eventType string) {
	if l.eventBus == nil {
		return
	}

	l.eventBus.Publish(sdk.Event{
		Type:   eventType,
		Source: l.plugin.Name(),
		Data: map[string]interface{}{
			"from_state": l.prevState,
			"to_state":   l.state,
			"timestamp":  time.Now().Unix(),
		},
	})
}

// GetInfo returns lifecycle info.
func (l *Lifecycle) GetInfo() map[string]interface{} {
	l.mu.RLock()
	defer l.mu.RUnlock()

	info := map[string]interface{}{
		"name":         l.plugin.Name(),
		"state":        l.state.String(),
		"attempts":     l.attempts,
		"max_attempts": l.maxAttempts,
		"timeout":      l.timeout.String(),
		"is_active":    l.state.IsActive(),
		"is_terminal":  l.state.IsTerminal(),
	}

	if !l.startedAt.IsZero() {
		info["started_at"] = l.startedAt
	}

	if !l.stoppedAt.IsZero() {
		info["stopped_at"] = l.stoppedAt
	}

	if l.lastError != nil {
		info["last_error"] = l.lastError.Error()
	}

	return info
}
