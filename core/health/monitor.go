// core/health/monitor.go
package health

import (
	"context"
	"sync"
	"time"

	"trailseal/core/permission"
	"trailseal/core/plugin"
	"trailseal/core/scheduler"
	"trailseal/sdk"
)

// Monitor adalah implementasi HealthMonitor
type Monitor struct {
	mu              sync.RWMutex
	pluginManager   *plugin.Manager
	scheduler       *scheduler.Scheduler
	permEngine      *permission.Engine
	eventBus        sdk.EventBus
	registry        sdk.Registry
	logger          sdk.Logger
	metricsCollector *MetricsCollector
	alertEngine     *AlertEngine
	interval        time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	isRunning       bool
	lastCheck       time.Time
	status          sdk.HealthStatus
	reports         []sdk.HealthReport
	checkCounter    int
	failCounter     int
	heartbeat       *Heartbeat
}

// NewMonitor membuat health monitor baru
func NewMonitor(
	pluginManager *plugin.Manager,
	scheduler *scheduler.Scheduler,
	permEngine *permission.Engine,
	eventBus sdk.EventBus,
	registry sdk.Registry,
	logger sdk.Logger,
) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())

	monitor := &Monitor{
		pluginManager: pluginManager,
		scheduler:     scheduler,
		permEngine:    permEngine,
		eventBus:      eventBus,
		registry:      registry,
		logger:        logger,
		interval:      30 * time.Second,
		ctx:           ctx,
		cancel:        cancel,
		isRunning:     false,
		status:        sdk.HealthUnknown,
		reports:       []sdk.HealthReport{},
		checkCounter:  0,
		failCounter:   0,
	}

	monitor.metricsCollector = NewMetricsCollector(
		pluginManager,
		scheduler,
		permEngine,
		eventBus,
		registry,
		logger,
	)

	monitor.alertEngine = NewAlertEngine(logger, monitor.metricsCollector)
	monitor.heartbeat = NewHeartbeat(logger)

	return monitor
}

// Start memulai monitoring
func (m *Monitor) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return nil
	}

	m.isRunning = true
	m.status = sdk.HealthUnknown

	// Start heartbeat
	m.heartbeat.Start()

	// Start monitoring loop
	go m.monitorLoop()

	m.logger.Info("Health monitor started", "interval", m.interval)
	return nil
}

// Stop menghentikan monitoring
func (m *Monitor) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	m.isRunning = false
	m.cancel()
	m.heartbeat.Stop()

	m.logger.Info("Health monitor stopped")
	return nil
}

// IsRunning mengecek apakah monitor berjalan
func (m *Monitor) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isRunning
}

// monitorLoop adalah loop monitoring
func (m *Monitor) monitorLoop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.doCheck()
		}
	}
}

// doCheck melakukan health check
func (m *Monitor) doCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.checkCounter++
	m.lastCheck = time.Now()

	// 1. Kumpulkan metrics
	metrics := m.metricsCollector.Collect()

	// 2. Evaluasi alert
	alerts := m.alertEngine.Evaluate(metrics)

	// 3. Buat report
	report := m.buildReport(metrics, alerts)

	// 4. Update status
	m.status = m.determineStatus(report)
	m.reports = append(m.reports, report)

	// 5. Log issues
	for _, alert := range alerts {
		if alert.Severity == "critical" {
			m.logger.Error("Health alert (critical)",
				"message", alert.Message,
				"metric", alert.Metric,
				"actual", alert.ActualValue,
				"threshold", alert.Threshold,
			)
		} else {
			m.logger.Warn("Health alert (warning)",
				"message", alert.Message,
				"metric", alert.Metric,
				"actual", alert.ActualValue,
				"threshold", alert.Threshold,
			)
		}
	}

	// 6. Heartbeat
	m.heartbeat.Ping()
}

// buildReport membuat laporan kesehatan
func (m *Monitor) buildReport(metrics []sdk.HealthMetric, alerts []sdk.HealthAlert) sdk.HealthReport {
	report := sdk.HealthReport{
		Component: "health-monitor",
		Status:    m.status,
		Timestamp: time.Now(),
		Metrics:   metrics,
		Details: map[string]interface{}{
			"total_checks": m.checkCounter,
			"alerts_count": len(alerts),
			"heartbeat":    m.heartbeat.Status(),
		},
	}

	// Add children reports
	children := []sdk.HealthReport{}
	if m.scheduler != nil {
		children = append(children, sdk.HealthReport{
			Component: "scheduler",
			Status:    m.getSchedulerHealth(),
			Timestamp: time.Now(),
		})
	}
	if m.pluginManager != nil {
		children = append(children, sdk.HealthReport{
			Component: "plugin-manager",
			Status:    m.getPluginManagerHealth(),
			Timestamp: time.Now(),
		})
	}
	if m.eventBus != nil {
		children = append(children, sdk.HealthReport{
			Component: "event-bus",
			Status:    m.getEventBusHealth(),
			Timestamp: time.Now(),
		})
	}
	if m.registry != nil {
		children = append(children, sdk.HealthReport{
			Component: "registry",
			Status:    m.getRegistryHealth(),
			Timestamp: time.Now(),
		})
	}
	report.Children = children

	return report
}

// determineStatus menentukan status berdasarkan report
func (m *Monitor) determineStatus(report sdk.HealthReport) sdk.HealthStatus {
	// Cek children status
	for _, child := range report.Children {
		if child.Status == sdk.HealthUnhealthy {
			return sdk.HealthUnhealthy
		}
	}

	// Cek alert
	for _, alert := range m.alertEngine.GetAlerts() {
		if alert.Severity == "critical" {
			return sdk.HealthUnhealthy
		}
	}

	return sdk.HealthHealthy
}

// CheckAll memeriksa semua komponen
func (m *Monitor) CheckAll() sdk.HealthReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.reports) == 0 {
		return sdk.HealthReport{
			Component: "health-monitor",
			Status:    sdk.HealthUnknown,
			Timestamp: time.Now(),
			Message:   "No checks performed yet",
		}
	}
	return m.reports[len(m.reports)-1]
}

// CheckComponent memeriksa komponen tertentu
func (m *Monitor) CheckComponent(name string) (sdk.HealthReport, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.reports) == 0 {
		return sdk.HealthReport{}, false
	}

	lastReport := m.reports[len(m.reports)-1]
	for _, child := range lastReport.Children {
		if child.Component == name {
			return child, true
		}
	}
	return sdk.HealthReport{}, false
}

// GetStatus mengembalikan status keseluruhan
func (m *Monitor) GetStatus() sdk.HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// GetAlerts mengembalikan alert yang aktif
func (m *Monitor) GetAlerts() []sdk.HealthAlert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.alertEngine.GetAlerts()
}

// GetMetrics mengembalikan metrics terbaru
func (m *Monitor) GetMetrics() []sdk.HealthMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metricsCollector.GetMetrics()
}

// getSchedulerHealth mengembalikan health status scheduler
func (m *Monitor) getSchedulerHealth() sdk.HealthStatus {
	if m.scheduler == nil {
		return sdk.HealthUnknown
	}
	// TODO: Implementasi scheduler health check
	return sdk.HealthHealthy
}

// getPluginManagerHealth mengembalikan health status plugin manager
func (m *Monitor) getPluginManagerHealth() sdk.HealthStatus {
	if m.pluginManager == nil {
		return sdk.HealthUnknown
	}
	return sdk.HealthHealthy
}

// getEventBusHealth mengembalikan health status event bus
func (m *Monitor) getEventBusHealth() sdk.HealthStatus {
	if m.eventBus == nil {
		return sdk.HealthUnknown
	}
	return sdk.HealthHealthy
}

// getRegistryHealth mengembalikan health status registry
func (m *Monitor) getRegistryHealth() sdk.HealthStatus {
	if m.registry == nil {
		return sdk.HealthUnknown
	}
	return sdk.HealthHealthy
}