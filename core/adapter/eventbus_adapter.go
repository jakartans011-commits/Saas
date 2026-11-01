// core/adapter/eventbus_adapter.go
package adapter

import (
	"sort"
	"sync"
	"time"

	"trailseal/sdk"
)

// subscriber adalah satu subscriber dengan priority
type subscriber struct {
	handler  func(sdk.Event)
	priority int
}

// EventBusAdapter adalah adapter dari sdk.EventBus
// Implementasi nyata dengan subscriber management, wildcard, dan async
type EventBusAdapter struct {
	mu           sync.RWMutex
	subscribers  map[string][]subscriber // eventType -> subscribers dengan priority
	eventHistory []sdk.Event
	historyLimit int
	stats        sdk.EventBusStats
	eventCh      chan sdk.Event
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// NewEventBusAdapter membuat adapter baru
func NewEventBusAdapter() sdk.EventBus {
	bus := &EventBusAdapter{
		subscribers:  make(map[string][]subscriber),
		eventHistory: []sdk.Event{},
		historyLimit: 1000,
		stats: sdk.EventBusStats{
			SubscribersByType: make(map[string]int),
			EventsByType:      make(map[string]int),
		},
		eventCh: make(chan sdk.Event, 1000), // buffer 1000 events
		stopCh:  make(chan struct{}),
	}

	// Start async worker
	bus.wg.Add(1)
	go bus.processEvents()

	return bus
}

// processEvents memproses event secara async
func (a *EventBusAdapter) processEvents() {
	defer a.wg.Done()
	for {
		select {
		case event := <-a.eventCh:
			a.publishSync(event)
		case <-a.stopCh:
			return
		}
	}
}

// Publish mengirim event secara sync
func (a *EventBusAdapter) Publish(event sdk.Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	a.publishSync(event)
}

// PublishAsync mengirim event secara async (non-blocking)
func (a *EventBusAdapter) PublishAsync(event sdk.Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Update stats
	a.mu.Lock()
	a.stats.TotalEvents++
	a.stats.EventsByType[event.Type]++
	a.mu.Unlock()

	// Kirim ke channel (non-blocking jika buffer penuh)
	select {
	case a.eventCh <- event:
	default:
		// Buffer penuh — fallback ke sync
		a.publishSync(event)
	}
}

// publishSync adalah implementasi sync publish
func (a *EventBusAdapter) publishSync(event sdk.Event) {
	// Update history
	a.mu.Lock()
	a.eventHistory = append(a.eventHistory, event)
	if len(a.eventHistory) > a.historyLimit {
		a.eventHistory = a.eventHistory[1:]
	}
	a.stats.TotalEvents++
	a.stats.EventsByType[event.Type]++
	a.mu.Unlock()

	// Dapatkan subscriber untuk event type
	a.mu.RLock()
	handlers := a.subscribers[event.Type]
	// Wildcard subscribers
	wildcardHandlers := a.subscribers["*"]
	a.mu.RUnlock()

	// Gabungkan dan sort by priority
	allHandlers := make([]subscriber, 0, len(handlers)+len(wildcardHandlers))
	allHandlers = append(allHandlers, handlers...)
	allHandlers = append(allHandlers, wildcardHandlers...)

	// Sort by priority (higher first)
	sort.Slice(allHandlers, func(i, j int) bool {
		return allHandlers[i].priority > allHandlers[j].priority
	})

	// Execute handlers
	for _, sub := range allHandlers {
		sub.handler(event)
	}
}

// Subscribe mendaftarkan handler untuk event type
func (a *EventBusAdapter) Subscribe(eventType string, handler func(sdk.Event)) {
	a.SubscribeWithPriority(eventType, 0, handler)
}

// SubscribeWithPriority mendaftarkan handler dengan priority
func (a *EventBusAdapter) SubscribeWithPriority(eventType string, priority int, handler func(sdk.Event)) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.subscribers[eventType] = append(a.subscribers[eventType], subscriber{
		handler:  handler,
		priority: priority,
	})
	a.stats.TotalSubscribers++
	a.stats.SubscribersByType[eventType]++
}

// Unsubscribe menghapus handler dari event type
func (a *EventBusAdapter) Unsubscribe(eventType string, handler func(sdk.Event)) {
	a.mu.Lock()
	defer a.mu.Unlock()

	handlers, ok := a.subscribers[eventType]
	if !ok {
		return
	}

	newHandlers := []subscriber{}
	for _, sub := range handlers {
		// Compare function pointers (simplified)
		if &sub.handler == &handler {
			continue
		}
		newHandlers = append(newHandlers, sub)
	}

	if len(newHandlers) == 0 {
		delete(a.subscribers, eventType)
		a.stats.SubscribersByType[eventType] = 0
	} else {
		a.subscribers[eventType] = newHandlers
		a.stats.SubscribersByType[eventType] = len(newHandlers)
	}
	a.stats.TotalSubscribers--
}

// GetSubscribersCount mengembalikan jumlah subscriber untuk event type
func (a *EventBusAdapter) GetSubscribersCount(eventType string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.subscribers[eventType])
}

// GetStats mengembalikan statistik event bus
func (a *EventBusAdapter) GetStats() sdk.EventBusStats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stats
}

// GetEventHistory mengembalikan riwayat event
func (a *EventBusAdapter) GetEventHistory(limit int) []sdk.Event {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if limit <= 0 || limit > len(a.eventHistory) {
		limit = len(a.eventHistory)
	}
	start := len(a.eventHistory) - limit
	return a.eventHistory[start:]
}

// Close menutup event bus (stop async processing)
func (a *EventBusAdapter) Close() {
	close(a.stopCh)
	a.wg.Wait()
}