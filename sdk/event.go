// sdk/event.go
package sdk

import "time"

// EventBus adalah interface untuk komunikasi antar module
// SEMUA komunikasi WAJIB melalui EventBus
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type EventBus interface {
	// Publish mengirim event ke semua subscriber
	Publish(event Event)

	// PublishAsync mengirim event secara async (non-blocking)
	PublishAsync(event Event)

	// Subscribe mendaftarkan handler untuk event type (support wildcard)
	Subscribe(eventType string, handler func(Event))

	// SubscribeWithPriority mendaftarkan handler dengan priority (0-100, higher = first)
	SubscribeWithPriority(eventType string, priority int, handler func(Event))

	// Unsubscribe menghapus handler dari event type
	Unsubscribe(eventType string, handler func(Event))

	// GetSubscribersCount mengembalikan jumlah subscriber untuk event type
	GetSubscribersCount(eventType string) int

	// GetStats mengembalikan statistik event bus
	GetStats() EventBusStats

	// GetEventHistory mengembalikan riwayat event (untuk debugging)
	GetEventHistory(limit int) []Event
}

// Event adalah data yang dikirim melalui EventBus
type Event struct {
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	TraceID   string                 `json:"trace_id,omitempty"`
	Priority  int                    `json:"priority,omitempty"` // 0-100
}

// EventBusStats adalah statistik event bus
type EventBusStats struct {
	TotalEvents       int            `json:"total_events"`
	TotalSubscribers  int            `json:"total_subscribers"`
	SubscribersByType map[string]int `json:"subscribers_by_type"`
	EventsByType      map[string]int `json:"events_by_type"`
}
