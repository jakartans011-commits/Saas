// core/health/metrics_collector.go
package health

import (
	"sync"
	"time"

	"trailseal/core/permission"
	"trailseal/core/plugin"
	"trailseal/core/scheduler"
	"trailseal/sdk"
)

// MetricsCollector mengumpulkan metrics dari semua komponen
type MetricsCollector struct {
	mu            sync.RWMutex
	pluginManager sdk.PluginManager
	scheduler     *scheduler.Scheduler
	permEngine    *permission.Engine
	eventBus      sdk.EventBus
	registry      sdk.Registry
	logger        sdk.Logger
	metrics       []sdk.HealthMetric
	history       map[string][]sdk.HealthMetric
	historyLimit  int
}

// NewMetricsCollector membuat metrics collector baru
func NewMetricsCollector(
	pluginManager sdk.PluginManager,
	scheduler *scheduler.Scheduler,
	permEngine *permission.Engine,
	eventBus sdk.EventBus,
	registry sdk.Registry,
	logger sdk.Logger,
) *MetricsCollector {
	return &MetricsCollector{
		pluginManager: pluginManager,
		scheduler:     scheduler,
		permEngine:    permEngine,
		eventBus:      eventBus,
		registry:      registry,
		logger:        logger,
		history:       make(map[string][]sdk.HealthMetric),
		historyLimit:  100,
	}
}

// Collect mengumpulkan semua metrics
func (c *MetricsCollector) Collect() []sdk.HealthMetric {
	c.mu.Lock()
	defer c.mu.Unlock()

	metrics := []sdk.HealthMetric{}

	// 1. Plugin metrics
	if c.pluginManager != nil {
		pluginMetrics := c.collectPluginMetrics()
		metrics = append(metrics, pluginMetrics...)
	}

	// 2. Scheduler metrics
	if c.scheduler != nil {
		schedulerMetrics := c.collectSchedulerMetrics()
		metrics = append(metrics, schedulerMetrics...)
	}

	// 3. Event Bus metrics
	if c.eventBus != nil {
		eventMetrics := c.collectEventBusMetrics()
		metrics = append(metrics, eventMetrics...)
	}

	// 4. Registry metrics
	if c.registry != nil {
		registryMetrics := c.collectRegistryMetrics()
		metrics = append(metrics, registryMetrics...)
	}

	c.metrics = metrics
	c.storeHistory(metrics)

	return metrics
}

// collectPluginMetrics mengumpulkan metrics dari plugin
func (c *MetricsCollector) collectPluginMetrics() []sdk.HealthMetric {
	metrics := []sdk.HealthMetric{}

	plugins := c.pluginManager.ListPlugins()
	running := c.pluginManager.ListRunning()
	loadOrder := c.pluginManager.GetLoadOrder()

	// Total plugins
	metrics = append(metrics, sdk.HealthMetric{
		Name:      "plugin.total",
		Value:     float64(len(plugins)),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	// Running plugins
	metrics = append(metrics, sdk.HealthMetric{
		Name:      "plugin.running",
		Value:     float64(len(running)),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	// Load order count
	metrics = append(metrics, sdk.HealthMetric{
		Name:      "plugin.load_order_count",
		Value:     float64(len(loadOrder)),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	// Health check per plugin
	for _, name := range plugins {
		instance, ok := c.pluginManager.GetPlugin(name)
		if !ok {
			continue
		}

		// 🔥 Type assertion ke *plugin.PluginInstance
		pluginInstance, ok := instance.(*plugin.PluginInstance)
		if !ok || pluginInstance == nil {
			continue
		}

		if pluginInstance.Plugin == nil {
			continue
		}

		err := pluginInstance.Plugin.Health()
		status := 1.0
		if err != nil {
			status = 0.0
		}
		metrics = append(metrics, sdk.HealthMetric{
			Name:  "plugin.health." + name,
			Value: status,
			Unit:  "boolean",
			Labels: map[string]string{
				"plugin": name,
			},
			Timestamp: time.Now(),
		})

		// Uptime
		if !pluginInstance.StartedAt.IsZero() {
			uptime := time.Since(pluginInstance.StartedAt).Seconds()
			metrics = append(metrics, sdk.HealthMetric{
				Name:  "plugin.uptime." + name,
				Value: uptime,
				Unit:  "seconds",
				Labels: map[string]string{
					"plugin": name,
				},
				Timestamp: time.Now(),
			})
		}
	}

	return metrics
}

// collectSchedulerMetrics mengumpulkan metrics dari scheduler
func (c *MetricsCollector) collectSchedulerMetrics() []sdk.HealthMetric {
	metrics := []sdk.HealthMetric{}
	stats := c.scheduler.Stats()

	metrics = append(metrics, sdk.HealthMetric{
		Name:      "scheduler.total_executables",
		Value:     float64(stats.TotalExecutables),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	metrics = append(metrics, sdk.HealthMetric{
		Name:      "scheduler.running",
		Value:     float64(stats.RunningCount),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	metrics = append(metrics, sdk.HealthMetric{
		Name:      "scheduler.pending",
		Value:     float64(stats.PendingCount),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	// Job success/fail rates dari stats status counts
	if stats.StatusCounts != nil {
		completed := float64(stats.StatusCounts[sdk.StatusCompleted])
		failed := float64(stats.StatusCounts[sdk.StatusFailed])
		total := completed + failed

		if total > 0 {
			// Success rate
			metrics = append(metrics, sdk.HealthMetric{
				Name:      "scheduler.job_success_rate",
				Value:     (completed / total) * 100,
				Unit:      "percent",
				Timestamp: time.Now(),
			})

			// Failure rate
			metrics = append(metrics, sdk.HealthMetric{
				Name:      "scheduler.job_failure_rate",
				Value:     (failed / total) * 100,
				Unit:      "percent",
				Timestamp: time.Now(),
			})
		}
	}

	return metrics
}

// collectEventBusMetrics mengumpulkan metrics dari event bus
func (c *MetricsCollector) collectEventBusMetrics() []sdk.HealthMetric {
	metrics := []sdk.HealthMetric{}
	stats := c.eventBus.GetStats()

	metrics = append(metrics, sdk.HealthMetric{
		Name:      "eventbus.total_events",
		Value:     float64(stats.TotalEvents),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	metrics = append(metrics, sdk.HealthMetric{
		Name:      "eventbus.total_subscribers",
		Value:     float64(stats.TotalSubscribers),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	// Events by type
	for eventType, count := range stats.EventsByType {
		metrics = append(metrics, sdk.HealthMetric{
			Name:  "eventbus.events." + eventType,
			Value: float64(count),
			Unit:  "count",
			Labels: map[string]string{
				"event_type": eventType,
			},
			Timestamp: time.Now(),
		})
	}

	return metrics
}

// collectRegistryMetrics mengumpulkan metrics dari registry
func (c *MetricsCollector) collectRegistryMetrics() []sdk.HealthMetric {
	metrics := []sdk.HealthMetric{}
	stats := c.registry.Stats()

	metrics = append(metrics, sdk.HealthMetric{
		Name:      "registry.total_plugins",
		Value:     float64(stats.TotalPlugins),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	metrics = append(metrics, sdk.HealthMetric{
		Name:      "registry.total_manifests",
		Value:     float64(stats.TotalManifests),
		Unit:      "count",
		Timestamp: time.Now(),
	})

	// Categories
	for category, count := range stats.Categories {
		metrics = append(metrics, sdk.HealthMetric{
			Name:  "registry.category." + category,
			Value: float64(count),
			Unit:  "count",
			Labels: map[string]string{
				"category": category,
			},
			Timestamp: time.Now(),
		})
	}

	// Permissions
	for perm, count := range stats.Permissions {
		metrics = append(metrics, sdk.HealthMetric{
			Name:  "registry.permission." + perm,
			Value: float64(count),
			Unit:  "count",
			Labels: map[string]string{
				"permission": perm,
			},
			Timestamp: time.Now(),
		})
	}

	return metrics
}

// storeHistory menyimpan metrics ke history
func (c *MetricsCollector) storeHistory(metrics []sdk.HealthMetric) {
	for _, m := range metrics {
		if _, ok := c.history[m.Name]; !ok {
			c.history[m.Name] = []sdk.HealthMetric{}
		}
		c.history[m.Name] = append(c.history[m.Name], m)
		if len(c.history[m.Name]) > c.historyLimit {
			c.history[m.Name] = c.history[m.Name][1:]
		}
	}
}

// GetHistory mengembalikan history metrics
func (c *MetricsCollector) GetHistory(name string, limit int) []sdk.HealthMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	history, ok := c.history[name]
	if !ok {
		return []sdk.HealthMetric{}
	}

	if len(history) > limit {
		return history[len(history)-limit:]
	}
	return history
}

// GetMetrics mengembalikan metrics terbaru
func (c *MetricsCollector) GetMetrics() []sdk.HealthMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics
}
