// core/metrics.go
package core

import (
    "net/http"
    "sync"
    
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsManager - manajemen metrics untuk Prometheus
type MetricsManager struct {
    mu sync.RWMutex
    
    // Counter
    BeaconSentTotal   prometheus.Counter
    BeaconFailedTotal prometheus.Counter
    ModuleStartTotal  prometheus.Counter
    ModuleFailedTotal prometheus.Counter
    
    // Gauge
    ActiveModules    prometheus.Gauge
    QueueSize        prometheus.Gauge
    GoroutineCount   prometheus.Gauge
    
    // Histogram
    BeaconLatency    prometheus.Histogram
    ModuleStartDuration prometheus.Histogram
}

// NewMetricsManager - membuat metrics manager
func NewMetricsManager() *MetricsManager {
    m := &MetricsManager{}
    
    // Register metrics
    m.BeaconSentTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "beacon_sent_total",
            Help: "Total number of beacons sent",
        },
    )
    
    m.BeaconFailedTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "beacon_failed_total",
            Help: "Total number of failed beacons",
        },
    )
    
    m.ModuleStartTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "module_start_total",
            Help: "Total number of module starts",
        },
    )
    
    m.ModuleFailedTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "module_failed_total",
            Help: "Total number of module failures",
        },
    )
    
    m.ActiveModules = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "active_modules",
            Help: "Number of active modules",
        },
    )
    
    m.QueueSize = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "queue_size",
            Help: "Current queue size",
        },
    )
    
    m.GoroutineCount = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "goroutine_count",
            Help: "Number of goroutines",
        },
    )
    
    m.BeaconLatency = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "beacon_latency_seconds",
            Help:    "Beacon request latency",
            Buckets: prometheus.DefBuckets,
        },
    )
    
    m.ModuleStartDuration = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "module_start_duration_seconds",
            Help:    "Module start duration",
            Buckets: prometheus.DefBuckets,
        },
    )
    
    // Register all metrics
    prometheus.MustRegister(
        m.BeaconSentTotal,
        m.BeaconFailedTotal,
        m.ModuleStartTotal,
        m.ModuleFailedTotal,
        m.ActiveModules,
        m.QueueSize,
        m.GoroutineCount,
        m.BeaconLatency,
        m.ModuleStartDuration,
    )
    
    return m
}

// StartServer - mulai metrics server
func (m *MetricsManager) StartServer(port string) {
    http.Handle("/metrics", promhttp.Handler())
    
    // Tambahkan endpoint health
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok"}`))
    })
    
    go func() {
        if err := http.ListenAndServe(":"+port, nil); err != nil {
            // Log error
        }
    }()
}