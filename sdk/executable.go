// sdk/executable.go
package sdk

import "time"

// Executable adalah unit kerja generik (Task, Job, Workflow, Pipeline)
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Executable interface {
	// Identity
	Name() string
	Description() string

	// Execute menjalankan executable
	Execute(ctx ExecutableContext) error

	// Cancel membatalkan executable yang sedang berjalan
	Cancel() error

	// Status mengembalikan status executable
	Status() ExecutableStatus
}

// ExecutableContext adalah context untuk executable
type ExecutableContext interface {
	// ID mengembalikan ID unik executable
	ID() string

	// PluginName mengembalikan nama plugin yang memiliki executable
	PluginName() string

	// TriggerType mengembalikan jenis trigger yang memicu executable
	TriggerType() string

	// Deadline mengembalikan deadline eksekusi
	Deadline() time.Time

	// SetValue menyimpan nilai ke context
	SetValue(key string, value interface{})

	// GetValue mengambil nilai dari context
	GetValue(key string) interface{}

	// IsCancelled mengecek apakah executable dibatalkan
	IsCancelled() bool

	// Cancel membatalkan executable dengan alasan
	Cancel(reason string)
}

// ExecutableStatus adalah status executable
type ExecutableStatus string

const (
	StatusPending   ExecutableStatus = "pending"
	StatusRunning   ExecutableStatus = "running"
	StatusCompleted ExecutableStatus = "completed"
	StatusFailed    ExecutableStatus = "failed"
	StatusCancelled ExecutableStatus = "cancelled"
	StatusTimeout   ExecutableStatus = "timeout"
)

// String mengembalikan string representation
func (s ExecutableStatus) String() string {
	return string(s)
}

// IsTerminal mengecek apakah status terminal
func (s ExecutableStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled || s == StatusTimeout
}

// IsActive mengecek apakah status aktif
func (s ExecutableStatus) IsActive() bool {
	return s == StatusPending || s == StatusRunning
}
