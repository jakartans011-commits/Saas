// core/scheduler/triggers/event.go
package triggers

import (
	"fmt"
	"time"

	"trailseal/sdk"
)

// EventTrigger adalah trigger berdasarkan event dari EventBus.
type EventTrigger struct {
	name      string
	eventType string
	enabled   bool
}

// NewEventTrigger membuat event trigger baru.
func NewEventTrigger(config map[string]interface{}) (sdk.Trigger, error) {
	eventType, ok := config["event_type"].(string)
	if !ok || eventType == "" {
		return nil, fmt.Errorf("event_type is required")
	}

	name, _ := config["name"].(string)
	if name == "" {
		name = fmt.Sprintf("event-%s", eventType)
	}

	enabled := true
	if v, ok := config["enabled"].(bool); ok {
		enabled = v
	}

	return &EventTrigger{
		name:      name,
		eventType: eventType,
		enabled:   enabled,
	}, nil
}

// Type mengembalikan jenis trigger.
func (t *EventTrigger) Type() string {
	return "event"
}

// Next mengembalikan waktu berikutnya trigger akan aktif.
//
// Event trigger tidak memiliki jadwal waktu berikutnya,
// sehingga selalu mengembalikan nil.
func (t *EventTrigger) Next(now time.Time) *time.Time {
	return nil
}

// Match mengecek apakah trigger aktif pada waktu tertentu.
//
// Event trigger tidak melakukan matching berdasarkan waktu,
// sehingga selalu false.
func (t *EventTrigger) Match(now time.Time) bool {
	return false
}

// Name mengembalikan nama trigger.
func (t *EventTrigger) Name() string {
	return t.name
}

// Enabled mengembalikan status trigger.
func (t *EventTrigger) Enabled() bool {
	return t.enabled
}

// SetEnabled mengaktifkan/menonaktifkan trigger.
func (t *EventTrigger) SetEnabled(enabled bool) {
	t.enabled = enabled
}

// EventType mengembalikan jenis event yang ditunggu.
func (t *EventTrigger) EventType() string {
	return t.eventType
}
