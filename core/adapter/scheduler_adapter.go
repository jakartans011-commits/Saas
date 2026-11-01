// core/adapter/scheduler_adapter.go
package adapter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"trailseal/sdk"
)

// SchedulerAdapter adalah adapter untuk scheduler (legacy)
type SchedulerAdapter struct {
	mu       sync.RWMutex
	tasks    map[string]*scheduledTask
	ctx      context.Context
	cancel   context.CancelFunc
	isClosed bool
	wg       sync.WaitGroup
}

type scheduledTask struct {
	name     string
	interval time.Duration
	handler  func(ctx context.Context)
	cancel   context.CancelFunc
	running  bool
}

// NewSchedulerAdapter membuat adapter baru
func NewSchedulerAdapter() sdk.Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &SchedulerAdapter{
		tasks:    make(map[string]*scheduledTask),
		ctx:      ctx,
		cancel:   cancel,
		isClosed: false,
	}
}

// ============================================================
// LEGACY METHODS
// ============================================================

// Schedule menjadwalkan task berulang
func (a *SchedulerAdapter) Schedule(name string, interval string, handler func(ctx context.Context)) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.isClosed {
		return fmt.Errorf("scheduler is closed")
	}

	if _, exists := a.tasks[name]; exists {
		return fmt.Errorf("task %s already exists", name)
	}

	duration, err := time.ParseDuration(interval)
	if err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}

	_, taskCancel := context.WithCancel(a.ctx)
	task := &scheduledTask{
		name:     name,
		interval: duration,
		handler:  handler,
		cancel:   taskCancel,
		running:  true,
	}

	a.tasks[name] = task

	a.wg.Add(1)
	go a.runTask(task)

	return nil
}

// ScheduleOnce menjadwalkan task sekali
func (a *SchedulerAdapter) ScheduleOnce(name string, delay string, handler func(ctx context.Context)) error {
	if a.isClosed {
		return fmt.Errorf("scheduler is closed")
	}

	duration, err := time.ParseDuration(delay)
	if err != nil {
		return fmt.Errorf("invalid delay: %w", err)
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(duration):
			handler(a.ctx)
		}
	}()

	return nil
}

// Cancel membatalkan task
func (a *SchedulerAdapter) Cancel(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	task, exists := a.tasks[name]
	if !exists {
		return fmt.Errorf("task %s not found", name)
	}

	task.cancel()
	task.running = false
	delete(a.tasks, name)

	return nil
}

// ============================================================
// SDK INTERFACE METHODS
// ============================================================

// List mengembalikan semua executable (legacy: return empty)
func (a *SchedulerAdapter) List() []sdk.Executable {
	return []sdk.Executable{}
}

// ListLegacy mengembalikan semua task names (untuk backward compatibility)
func (a *SchedulerAdapter) ListLegacy() ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	names := make([]string, 0, len(a.tasks))
	for name := range a.tasks {
		names = append(names, name)
	}
	return names, nil
}

// Status mengembalikan status task (sesuai SDK)
func (a *SchedulerAdapter) Status(name string) (sdk.ExecutableStatus, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	task, exists := a.tasks[name]
	if !exists {
		return "", fmt.Errorf("task %s not found", name)
	}

	if task.running {
		return sdk.StatusRunning, nil
	}
	return sdk.StatusCompleted, nil
}

// StatusLegacy mengembalikan status task sebagai string
func (a *SchedulerAdapter) StatusLegacy(name string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	task, exists := a.tasks[name]
	if !exists {
		return "", fmt.Errorf("task %s not found", name)
	}

	if task.running {
		return "running", nil
	}
	return "stopped", nil
}

// Register mendaftarkan executable (legacy - tidak digunakan)
func (a *SchedulerAdapter) Register(executable sdk.Executable) error {
	return fmt.Errorf("legacy scheduler does not support executable registration")
}

// Unregister menghapus executable (legacy - tidak digunakan)
func (a *SchedulerAdapter) Unregister(name string) error {
	return fmt.Errorf("legacy scheduler does not support executable unregistration")
}

// Get mengembalikan executable (legacy - tidak digunakan)
func (a *SchedulerAdapter) Get(name string) (sdk.Executable, bool) {
	return nil, false
}

// ListByPlugin mengembalikan executable untuk plugin (legacy - tidak digunakan)
func (a *SchedulerAdapter) ListByPlugin(pluginName string) []sdk.Executable {
	return []sdk.Executable{}
}

// Trigger secara manual menjalankan task (legacy)
func (a *SchedulerAdapter) Trigger(name string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	task, exists := a.tasks[name]
	if !exists {
		return fmt.Errorf("task %s not found", name)
	}

	if !task.running {
		return fmt.Errorf("task %s is not running", name)
	}

	go task.handler(a.ctx)
	return nil
}

// Stats mengembalikan statistik scheduler (legacy)
func (a *SchedulerAdapter) Stats() sdk.SchedulerStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := sdk.SchedulerStats{
		TotalExecutables: len(a.tasks),
		StatusCounts:     make(map[sdk.ExecutableStatus]int),
	}

	running := 0
	for _, task := range a.tasks {
		if task.running {
			running++
		}
	}
	stats.RunningCount = running
	stats.PendingCount = len(a.tasks) - running

	return stats
}

// ============================================================
// 🔥 LIFECYCLE METHODS (untuk kompatibilitas SDK baru)
// ============================================================

// Start memulai scheduler (legacy - sudah berjalan sejak awal)
func (a *SchedulerAdapter) Start(ctx context.Context) error {
	// Legacy scheduler sudah berjalan, tidak perlu melakukan apa-apa
	return nil
}

// Stop menghentikan scheduler (legacy - close)
func (a *SchedulerAdapter) Stop() error {
	a.Close()
	return nil
}

// ============================================================
// INTERNAL METHODS
// ============================================================

func (a *SchedulerAdapter) runTask(task *scheduledTask) {
	defer a.wg.Done()

	ticker := time.NewTicker(task.interval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			task.handler(a.ctx)
		}
	}
}

// Close menutup scheduler
func (a *SchedulerAdapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.isClosed {
		return
	}

	a.isClosed = true
	a.cancel()

	for _, task := range a.tasks {
		task.cancel()
	}
	a.tasks = make(map[string]*scheduledTask)
	a.wg.Wait()
}
