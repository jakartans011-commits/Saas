// core/scheduler/triggers/manual.go
package triggers

import (
	"time"

	"trailseal/sdk"
)

// ManualTrigger adalah trigger yang hanya aktif ketika dipanggil secara manual
type ManualTrigger struct {
	name    string
	enabled bool
}

// NewManualTrigger membuat manual trigger baru
func NewManualTrigger(config map[string]interface{}) (sdk.Trigger, error) {
	name, _ := config["name"].(string)
	if name == "" {
		name = "manual"
	}

	enabled := true
	if v, ok := config["enabled"].(bool); ok {
		enabled = v
	}

	return &ManualTrigger{
		name:    name,
		enabled: enabled,
	}, nil
}

// Type mengembalikan jenis trigger
func (t *ManualTrigger) Type() string {
	return "manual"
}

// Next mengembalikan waktu berikutnya trigger akan aktif
func (t *ManualTrigger) Next(now time.Time) *time.Time {
	// Manual trigger tidak memiliki waktu berikutnya
	return nil
}

// Match mengecek apakah trigger aktif pada waktu tertentu
func (t *ManualTrigger) Match(now time.Time) bool {
	// Manual trigger tidak match berdasarkan waktu
	return false
}

// Name mengembalikan nama trigger
func (t *ManualTrigger) Name() string {
	return t.name
}

// Enabled mengembalikan status trigger
func (t *ManualTrigger) Enabled() bool {
	return t.enabled
}

// SetEnabled mengaktifkan/menonaktifkan trigger
func (t *ManualTrigger) SetEnabled(enabled bool) {
	t.enabled = enabled
}