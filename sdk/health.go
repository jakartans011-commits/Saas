// sdk/health.go
package sdk

import "time"

// HealthStatus adalah status kesehatan
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthUnknown   HealthStatus = "unknown"
)

// HealthCheckable adalah interface untuk komponen yang bisa di-check kesehatannya
type HealthCheckable interface {
	Health() error
}

// HealthMetric adalah metrik kesehatan
type HealthMetric struct {
	Name      string                 `json:"name"`
	Value     float64                `json:"value"`
	Unit      string                 `json:"unit"`
	Labels    map[string]string      `json:"labels,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// HealthReport adalah laporan kesehatan
type HealthReport struct {
	Component   string                 `json:"component"`
	Status      HealthStatus           `json:"status"`
	Message     string                 `json:"message,omitempty"`
	Metrics     []HealthMetric         `json:"metrics,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Children    []HealthReport         `json:"children,omitempty"`
}

// HealthAlert adalah alert kesehatan
type HealthAlert struct {
	ID          string                 `json:"id"`
	Severity    string                 `json:"severity"` // "warning", "critical"
	Message     string                 `json:"message"`
	Component   string                 `json:"component"`
	Metric      string                 `json:"metric"`
	Threshold   float64                `json:"threshold"`
	ActualValue float64                `json:"actual_value"`
	Timestamp   time.Time              `json:"timestamp"`
	Resolved    bool                   `json:"resolved"`
	ResolvedAt  *time.Time             `json:"resolved_at,omitempty"`
}

// HealthMonitor adalah interface untuk health monitor
type HealthMonitor interface {
	// CheckAll memeriksa semua komponen
	CheckAll() HealthReport

	// CheckComponent memeriksa komponen tertentu
	CheckComponent(name string) (HealthReport, bool)

	// GetStatus mengembalikan status keseluruhan
	GetStatus() HealthStatus

	// GetAlerts mengembalikan alert yang aktif
	GetAlerts() []HealthAlert

	// GetMetrics mengembalikan metrics terbaru
	GetMetrics() []HealthMetric

	// Start memulai monitoring
	Start() error

	// Stop menghentikan monitoring
	Stop() error

	// IsRunning mengecek apakah monitor berjalan
	IsRunning() bool
}