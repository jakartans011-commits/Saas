// sdk/scheduler.go
package sdk

import "context"

// Scheduler adalah interface untuk scheduling executable
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Scheduler interface {
	// Register mendaftarkan executable
	Register(executable Executable) error

	// Unregister menghapus executable
	Unregister(name string) error

	// Get mengembalikan executable berdasarkan nama
	Get(name string) (Executable, bool)

	// List mengembalikan semua executable
	List() []Executable

	// ListByPlugin mengembalikan executable untuk plugin tertentu
	ListByPlugin(pluginName string) []Executable

	// Trigger secara manual menjalankan executable
	Trigger(name string) error

	// Cancel membatalkan executable yang sedang berjalan
	Cancel(name string) error

	// Status mengembalikan status executable
	Status(name string) (ExecutableStatus, error)

	// Stats mengembalikan statistik scheduler
	Stats() SchedulerStats

	// 🔥 LIFECYCLE — untuk start/stop scheduler
	Start(ctx context.Context) error
	Stop() error
}

// SchedulerStats adalah statistik scheduler
type SchedulerStats struct {
	TotalExecutables int                      `json:"total_executables"`
	RunningCount     int                      `json:"running_count"`
	PendingCount     int                      `json:"pending_count"`
	CompletedCount   int                      `json:"completed_count"`
	FailedCount      int                      `json:"failed_count"`
	CancelledCount   int                      `json:"cancelled_count"`
	TimeoutCount     int                      `json:"timeout_count"`
	StatusCounts     map[ExecutableStatus]int `json:"status_counts"`
}
