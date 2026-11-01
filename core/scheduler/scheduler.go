// core/scheduler/scheduler.go
package scheduler

import (
	"context" // ← TAMBAHKAN INI!
	"fmt"
	"sync"
	"time"

	"trailseal/sdk"
)

// Scheduler adalah implementasi sdk.Scheduler
type Scheduler struct {
	mu          sync.RWMutex
	store       *TaskStore
	workerPool  *WorkerPool
	executor    *Executor
	logger      sdk.Logger
	config      *SchedulerConfig
	triggers    map[string]sdk.Trigger
	executables map[string]sdk.Executable
	stats       sdk.SchedulerStats
	isRunning   bool
	stopCh      chan struct{}
}

// NewScheduler membuat scheduler baru
func NewScheduler(
	logger sdk.Logger,
	config *SchedulerConfig,
) *Scheduler {
	if config == nil {
		config = DefaultSchedulerConfig()
	}
	config.Validate()

	store := NewTaskStore()
	executor := NewExecutor(logger, config)
	workerPool := NewWorkerPool(config.WorkerPoolSize, executor)

	return &Scheduler{
		store:       store,
		workerPool:  workerPool,
		executor:    executor,
		logger:      logger,
		config:      config,
		triggers:    make(map[string]sdk.Trigger),
		executables: make(map[string]sdk.Executable),
		stats:       sdk.SchedulerStats{StatusCounts: make(map[sdk.ExecutableStatus]int)},
		stopCh:      make(chan struct{}),
		isRunning:   false,
	}
}

// Start memulai scheduler
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return nil
	}

	s.isRunning = true
	s.workerPool.Start(ctx)

	s.logger.Info("Scheduler started",
		"workers", s.config.WorkerPoolSize,
		"queue_size", s.config.QueueSize,
	)

	// Start trigger evaluator
	go s.evaluateTriggers(ctx)

	return nil
}

// Stop menghentikan scheduler
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return nil
	}

	s.isRunning = false
	close(s.stopCh)
	s.workerPool.Stop()

	s.logger.Info("Scheduler stopped")
	return nil
}

// Register mendaftarkan executable
func (s *Scheduler) Register(executable sdk.Executable) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := executable.Name()
	if _, exists := s.executables[name]; exists {
		return fmt.Errorf("executable %s already registered", name)
	}

	s.executables[name] = executable
	s.stats.TotalExecutables++

	s.logger.Info("Executable registered",
		"name", name,
		"description", executable.Description(),
	)

	return nil
}

// Unregister menghapus executable
func (s *Scheduler) Unregister(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.executables[name]; !exists {
		return fmt.Errorf("executable %s not found", name)
	}

	delete(s.executables, name)
	s.stats.TotalExecutables--

	s.logger.Info("Executable unregistered", "name", name)
	return nil
}

// Get mengembalikan executable
func (s *Scheduler) Get(name string) (sdk.Executable, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exec, ok := s.executables[name]
	return exec, ok
}

// List mengembalikan semua executable
func (s *Scheduler) List() []sdk.Executable {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]sdk.Executable, 0, len(s.executables))
	for _, exec := range s.executables {
		result = append(result, exec)
	}
	return result
}

// ListByPlugin mengembalikan executable untuk plugin tertentu
func (s *Scheduler) ListByPlugin(pluginName string) []sdk.Executable {
	// TODO: Implementasi
	return []sdk.Executable{}
}

// Trigger secara manual menjalankan executable
func (s *Scheduler) Trigger(name string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, ok := s.executables[name]
	if !ok {
		return fmt.Errorf("executable %s not found", name)
	}

	// Buat context untuk manual trigger
	ctx := NewExecutableContext(name, "manual", "manual", time.Now().Add(s.config.DefaultTimeout))
	s.workerPool.Submit(exec, ctx)

	s.logger.Info("Executable triggered manually", "name", name)
	return nil
}

// Cancel membatalkan executable
func (s *Scheduler) Cancel(name string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, ok := s.executables[name]
	if !ok {
		return fmt.Errorf("executable %s not found", name)
	}

	return exec.Cancel()
}

// Status mengembalikan status executable
func (s *Scheduler) Status(name string) (sdk.ExecutableStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, ok := s.executables[name]
	if !ok {
		return "", fmt.Errorf("executable %s not found", name)
	}

	return exec.Status(), nil
}

// Stats mengembalikan statistik scheduler
func (s *Scheduler) Stats() sdk.SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// evaluateTriggers mengevaluasi trigger secara periodik
func (s *Scheduler) evaluateTriggers(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.checkTriggers(now)
		}
	}
}

// checkTriggers memeriksa trigger yang aktif
func (s *Scheduler) checkTriggers(now time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for name, exec := range s.executables {
		// Untuk sekarang, trigger sederhana: interval 30s
		// TODO: Implementasi trigger dari manifest
		_ = name
		_ = exec
	}
}
