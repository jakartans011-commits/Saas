// core/scheduler/triggers/interval.go
package triggers

import (
	"fmt"
	"time"

	"trailseal/sdk"
)

// IntervalTrigger adalah trigger berdasarkan interval
type IntervalTrigger struct {
	name     string
	interval time.Duration
	enabled  bool
	lastRun  time.Time
}

// NewIntervalTrigger membuat interval trigger baru
func NewIntervalTrigger(config map[string]interface{}) (sdk.Trigger, error) {
	intervalStr, ok := config["interval"].(string)
	if !ok {
		return nil, fmt.Errorf("interval is required")
	}

	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return nil, fmt.Errorf("invalid interval: %w", err)
	}

	name, _ := config["name"].(string)
	if name == "" {
		name = fmt.Sprintf("interval-%s", intervalStr)
	}

	enabled := true
	if v, ok := config["enabled"].(bool); ok {
		enabled = v
	}

	return &IntervalTrigger{
		name:     name,
		interval: interval,
		enabled:  enabled,
		lastRun:  time.Time{},
	}, nil
}

// Type mengembalikan jenis trigger
func (t *IntervalTrigger) Type() string {
	return "interval"
}

// Next mengembalikan waktu berikutnya trigger akan aktif
func (t *IntervalTrigger) Next(now time.Time) *time.Time {
	if !t.enabled {
		return nil
	}

	next := t.lastRun.Add(t.interval)
	if next.Before(now) {
		next = now
	}
	return &next
}

// Match mengecek apakah trigger aktif pada waktu tertentu
func (t *IntervalTrigger) Match(now time.Time) bool {
	if !t.enabled {
		return false
	}

	if t.lastRun.IsZero() {
		return true
	}

	if now.Sub(t.lastRun) >= t.interval {
		t.lastRun = now
		return true
	}

	return false
}

// Name mengembalikan nama trigger
func (t *IntervalTrigger) Name() string {
	return t.name
}

// Enabled mengembalikan status trigger
func (t *IntervalTrigger) Enabled() bool {
	return t.enabled
}

// SetEnabled mengaktifkan/menonaktifkan trigger
func (t *IntervalTrigger) SetEnabled(enabled bool) {
	t.enabled = enabled
}
