// core/scheduler/config.go
package scheduler

import "time"

// SchedulerConfig adalah konfigurasi scheduler
type SchedulerConfig struct {
	// WorkerPoolSize jumlah worker concurrent (default: 10)
	WorkerPoolSize int `json:"worker_pool_size"`

	// MaxTasksPerPlugin maksimal task per plugin (default: 10)
	MaxTasksPerPlugin int `json:"max_tasks_per_plugin"`

	// DefaultTimeout default timeout per task (default: 30s)
	DefaultTimeout time.Duration `json:"default_timeout"`

	// MaxRetryAttempts maksimal retry attempts (default: 3)
	MaxRetryAttempts int `json:"max_retry_attempts"`

	// QueueSize ukuran queue untuk pending tasks (default: 1000)
	QueueSize int `json:"queue_size"`

	// BackoffPolicy kebijakan backoff ("linear", "exponential") (default: "exponential")
	BackoffPolicy string `json:"backoff_policy"`

	// GracefulTimeout timeout untuk graceful shutdown (default: 10s)
	GracefulTimeout time.Duration `json:"graceful_timeout"`

	// EnableMetrics mengaktifkan metrics (default: true)
	EnableMetrics bool `json:"enable_metrics"`
}

// DefaultSchedulerConfig mengembalikan konfigurasi default
func DefaultSchedulerConfig() *SchedulerConfig {
	return &SchedulerConfig{
		WorkerPoolSize:    10,
		MaxTasksPerPlugin: 10,
		DefaultTimeout:    30 * time.Second,
		MaxRetryAttempts:  3,
		QueueSize:         1000,
		BackoffPolicy:     "exponential",
		GracefulTimeout:   10 * time.Second,
		EnableMetrics:     true,
	}
}

// Validate memvalidasi konfigurasi
func (c *SchedulerConfig) Validate() error {
	if c.WorkerPoolSize <= 0 {
		c.WorkerPoolSize = 10
	}
	if c.MaxTasksPerPlugin <= 0 {
		c.MaxTasksPerPlugin = 10
	}
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = 30 * time.Second
	}
	if c.MaxRetryAttempts <= 0 {
		c.MaxRetryAttempts = 3
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 1000
	}
	if c.BackoffPolicy == "" {
		c.BackoffPolicy = "exponential"
	}
	if c.GracefulTimeout <= 0 {
		c.GracefulTimeout = 10 * time.Second
	}
	return nil
}
