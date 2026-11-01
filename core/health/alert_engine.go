// core/health/alert_engine.go
package health

import (
	"sync"
	"time"

	"trailseal/sdk"
)

// AlertRule adalah aturan alert
type AlertRule struct {
	ID          string
	MetricName  string
	Severity    string // "warning", "critical"
	Threshold   float64
	Operator    string        // "gt", "lt", "gte", "lte", "eq"
	Duration    time.Duration // Durasi kondisi harus terpenuhi
	Cooldown    time.Duration // Cooldown setelah alert
	Description string
}

// AlertEngine mengevaluasi threshold dan menghasilkan alert
type AlertEngine struct {
	mu          sync.RWMutex
	rules       []AlertRule
	alerts      map[string][]sdk.HealthAlert
	logger      sdk.Logger
	metricStore *MetricsCollector
	lastEval    map[string]time.Time
}

// NewAlertEngine membuat alert engine baru
func NewAlertEngine(logger sdk.Logger, metricStore *MetricsCollector) *AlertEngine {
	engine := &AlertEngine{
		rules:       []AlertRule{},
		alerts:      make(map[string][]sdk.HealthAlert),
		logger:      logger,
		metricStore: metricStore,
		lastEval:    make(map[string]time.Time),
	}

	// 🔥 Default rules
	engine.loadDefaultRules()
	return engine
}

// loadDefaultRules memuat aturan default
func (e *AlertEngine) loadDefaultRules() {
	e.rules = []AlertRule{
		// Permission Deny Rate
		{
			ID:          "permission-deny-rate-warning",
			MetricName:  "permission.deny_rate",
			Severity:    "warning",
			Threshold:   10.0,
			Operator:    "gt",
			Duration:    5 * time.Minute,
			Cooldown:    5 * time.Minute,
			Description: "Permission deny rate > 10% in 5 minutes",
		},
		{
			ID:          "permission-deny-rate-critical",
			MetricName:  "permission.deny_rate",
			Severity:    "critical",
			Threshold:   30.0,
			Operator:    "gt",
			Duration:    5 * time.Minute,
			Cooldown:    5 * time.Minute,
			Description: "Permission deny rate > 30% in 5 minutes - possible attack",
		},
		// Job Failure Rate
		{
			ID:          "job-failure-rate-warning",
			MetricName:  "scheduler.job_failure_rate",
			Severity:    "warning",
			Threshold:   20.0,
			Operator:    "gt",
			Duration:    10 * time.Minute,
			Cooldown:    5 * time.Minute,
			Description: "Job failure rate > 20% in 10 minutes",
		},
		{
			ID:          "job-failure-rate-critical",
			MetricName:  "scheduler.job_failure_rate",
			Severity:    "critical",
			Threshold:   50.0,
			Operator:    "gt",
			Duration:    10 * time.Minute,
			Cooldown:    5 * time.Minute,
			Description: "Job failure rate > 50% in 10 minutes - plugin needs restart",
		},
		// Plugin Health
		{
			ID:          "plugin-unhealthy-critical",
			MetricName:  "plugin.health.*",
			Severity:    "critical",
			Threshold:   0.0,
			Operator:    "eq",
			Duration:    30 * time.Second,
			Cooldown:    1 * time.Minute,
			Description: "Plugin health check failed 3 times consecutively",
		},
	}
}

// Evaluate mengevaluasi semua rules
func (e *AlertEngine) Evaluate(metrics []sdk.HealthMetric) []sdk.HealthAlert {
	e.mu.Lock()
	defer e.mu.Unlock()

	generatedAlerts := []sdk.HealthAlert{}

	for _, rule := range e.rules {
		// Cari metrics yang sesuai dengan rule
		matchingMetrics := []sdk.HealthMetric{}
		for _, m := range metrics {
			if m.Name == rule.MetricName {
				matchingMetrics = append(matchingMetrics, m)
			}
		}

		if len(matchingMetrics) == 0 {
			continue
		}

		for _, m := range matchingMetrics {
			// Cek apakah sudah ada alert untuk rule ini
			alertKey := rule.ID + ":" + m.Name
			if existingAlerts, ok := e.alerts[alertKey]; ok {
				// Cek cooldown
				if len(existingAlerts) > 0 {
					lastAlert := existingAlerts[len(existingAlerts)-1]
					if !lastAlert.Resolved {
						// Alert masih aktif, cek apakah sudah lewat cooldown
						if time.Since(lastAlert.Timestamp) < rule.Cooldown {
							continue
						}
					}
				}
			}

			// Evaluasi threshold
			if e.evaluateThreshold(m.Value, rule.Threshold, rule.Operator) {
				alert := sdk.HealthAlert{
					ID:          rule.ID + ":" + m.Name,
					Severity:    rule.Severity,
					Message:     rule.Description,
					Component:   "health-monitor",
					Metric:      m.Name,
					Threshold:   rule.Threshold,
					ActualValue: m.Value,
					Timestamp:   time.Now(),
					Resolved:    false,
				}
				generatedAlerts = append(generatedAlerts, alert)
				e.alerts[alertKey] = append(e.alerts[alertKey], alert)
			}
		}
	}

	return generatedAlerts
}

// evaluateThreshold mengevaluasi threshold dengan operator
func (e *AlertEngine) evaluateThreshold(value, threshold float64, operator string) bool {
	switch operator {
	case "gt":
		return value > threshold
	case "lt":
		return value < threshold
	case "gte":
		return value >= threshold
	case "lte":
		return value <= threshold
	case "eq":
		return value == threshold
	default:
		return false
	}
}

// GetAlerts mengembalikan semua alert
func (e *AlertEngine) GetAlerts() []sdk.HealthAlert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := []sdk.HealthAlert{}
	for _, alerts := range e.alerts {
		for _, alert := range alerts {
			if !alert.Resolved {
				result = append(result, alert)
			}
		}
	}
	return result
}

// ResolveAlert menandai alert sebagai resolved
func (e *AlertEngine) ResolveAlert(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if alerts, ok := e.alerts[id]; ok {
		for i := range alerts {
			if !alerts[i].Resolved {
				alerts[i].Resolved = true
				now := time.Now()
				alerts[i].ResolvedAt = &now
			}
		}
	}
}
