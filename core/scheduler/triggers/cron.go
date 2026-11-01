// core/scheduler/triggers/cron.go
package triggers

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

	"trailseal/sdk"
)

// CronTrigger adalah trigger berdasarkan cron expression
type CronTrigger struct {
	name     string
	cronExpr string
	schedule cron.Schedule
	enabled  bool
}

// NewCronTrigger membuat cron trigger baru
func NewCronTrigger(config map[string]interface{}) (sdk.Trigger, error) {
	cronExpr, ok := config["cron"].(string)
	if !ok {
		return nil, fmt.Errorf("cron expression is required")
	}

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	name, _ := config["name"].(string)
	if name == "" {
		name = fmt.Sprintf("cron-%s", cronExpr)
	}

	enabled := true
	if v, ok := config["enabled"].(bool); ok {
		enabled = v
	}

	return &CronTrigger{
		name:     name,
		cronExpr: cronExpr,
		schedule: schedule,
		enabled:  enabled,
	}, nil
}

// Type mengembalikan jenis trigger
func (t *CronTrigger) Type() string {
	return "cron"
}

// Next mengembalikan waktu berikutnya trigger akan aktif
func (t *CronTrigger) Next(now time.Time) *time.Time {
	if !t.enabled {
		return nil
	}

	next := t.schedule.Next(now)
	return &next
}

// Match mengecek apakah trigger aktif pada waktu tertentu
func (t *CronTrigger) Match(now time.Time) bool {
	if !t.enabled {
		return false
	}

	// Cron trigger match jika now adalah waktu yang dijadwalkan
	// Implementasi sederhana: cek apakah now sama dengan waktu berikutnya
	next := t.schedule.Next(now)
	if now.Equal(next) || now.After(next) {
		return true
	}
	return false
}

// Name mengembalikan nama trigger
func (t *CronTrigger) Name() string {
	return t.name
}

// Enabled mengembalikan status trigger
func (t *CronTrigger) Enabled() bool {
	return t.enabled
}

// SetEnabled mengaktifkan/menonaktifkan trigger
func (t *CronTrigger) SetEnabled(enabled bool) {
	t.enabled = enabled
}