// core/scheduler/executor.go
package scheduler

import (
	"context"
	"time"

	"trailseal/sdk"
)

// Executor adalah eksekutor untuk executable
type Executor struct {
	logger sdk.Logger
	config *SchedulerConfig
}

// NewExecutor membuat executor baru
func NewExecutor(logger sdk.Logger, config *SchedulerConfig) *Executor {
	return &Executor{
		logger: logger,
		config: config,
	}
}

// Execute mengeksekusi executable
func (e *Executor) Execute(executable sdk.Executable, ctx sdk.ExecutableContext) {
	startTime := time.Now()

	e.logger.Info("Executing executable",
		"name", executable.Name(),
		"plugin", ctx.PluginName(),
		"trigger", ctx.TriggerType(),
	)

	// Cek deadline
	deadline := ctx.Deadline()
	if !deadline.IsZero() && time.Now().After(deadline) {
		e.logger.Warn("Executable deadline exceeded",
			"name", executable.Name(),
			"deadline", deadline,
		)
		return
	}

	// Execute dengan timeout
	execCtx, cancel := context.WithTimeout(context.Background(), e.config.DefaultTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- executable.Execute(ctx)
	}()

	select {
	case err := <-done:
		execTime := time.Since(startTime)
		if err != nil {
			e.logger.Error("Executable failed",
				"name", executable.Name(),
				"error", err,
				"duration", execTime,
			)
		} else {
			e.logger.Info("Executable completed",
				"name", executable.Name(),
				"duration", execTime,
			)
		}
	case <-execCtx.Done():
		e.logger.Warn("Executable timeout",
			"name", executable.Name(),
			"timeout", e.config.DefaultTimeout,
		)
	}
}
