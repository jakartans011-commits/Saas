// core/eventbus.go
package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ============================================================================
// EVENT DEFINITION
// ============================================================================

type EventType string

type Event struct {
	Type      EventType              `json:"type"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

type EventHandler func(ctx context.Context, event Event) error

// ============================================================================
// EVENT BUS
// ============================================================================

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]*Subscription
	logger      *slog.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	eventQueue  chan Event
	queueSize   int
}

type Subscription struct {
	ID       string
	Module   string
	Handler  EventHandler
	Active   bool
	Created  time.Time
	mu       sync.RWMutex
}

// ============================================================================
// NEW EVENT BUS - SATU SATUNYA!
// ============================================================================

func NewEventBus() *EventBus {
	logger := slog.Default()
	ctx, cancel := context.WithCancel(context.Background())

	eb := &EventBus{
		subscribers: make(map[EventType][]*Subscription),
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		eventQueue:  make(chan Event, 1000),
		queueSize:   1000,
	}

	eb.wg.Add(1)
	go eb.processEvents()

	logger.Info("Event bus initialized", "queue_size", 1000)
	return eb
}

// ============================================================================
// SUBSCRIBE
// ============================================================================

func (eb *EventBus) Subscribe(module string, eventType EventType, handler EventHandler) string {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subID := fmt.Sprintf("%s-%d", module, time.Now().UnixNano())

	sub := &Subscription{
		ID:      subID,
		Module:  module,
		Handler: handler,
		Active:  true,
		Created: time.Now(),
	}

	eb.subscribers[eventType] = append(eb.subscribers[eventType], sub)

	eb.logger.Debug("Subscription added",
		"module", module,
		"event_type", eventType,
		"subscription_id", subID,
	)

	return subID
}

// ============================================================================
// PUBLISH - DENGAN CONTEXT DARI eventbus_impl
// ============================================================================

func (eb *EventBus) Publish(ctx context.Context, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case eb.eventQueue <- event:
		eb.logger.Debug("Event published",
			"type", event.Type,
			"source", event.Source,
		)
		return nil
	case <-time.After(100 * time.Millisecond):
		return fmt.Errorf("event queue full, event dropped: %s", event.Type)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ============================================================================
// UNSUBSCRIBE
// ============================================================================

func (eb *EventBus) Unsubscribe(subID string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for eventType, subs := range eb.subscribers {
		for i, sub := range subs {
			if sub.ID == subID {
				sub.mu.Lock()
				sub.Active = false
				sub.mu.Unlock()

				eb.subscribers[eventType] = append(subs[:i], subs[i+1:]...)

				eb.logger.Debug("Subscription removed",
					"subscription_id", subID,
					"event_type", eventType,
				)
				return nil
			}
		}
	}

	return fmt.Errorf("subscription %s not found", subID)
}

// ============================================================================
// EVENT PROCESSOR
// ============================================================================

func (eb *EventBus) processEvents() {
	defer eb.wg.Done()

	eb.logger.Info("Event processor started")

	for {
		select {
		case <-eb.ctx.Done():
			eb.logger.Info("Event processor stopped")
			return
		case event := <-eb.eventQueue:
			eb.processEvent(event)
		}
	}
}

func (eb *EventBus) processEvent(event Event) {
	eb.mu.RLock()
	subs := eb.getSubscribers(event.Type)
	eb.mu.RUnlock()

	if len(subs) == 0 {
		return
	}

	for _, sub := range subs {
		sub.mu.RLock()
		active := sub.Active
		handler := sub.Handler
		module := sub.Module
		sub.mu.RUnlock()

		if !active {
			continue
		}

		ctx, cancel := context.WithTimeout(eb.ctx, 10*time.Second)

		go func(subID, mod string, h EventHandler, ev Event) {
			defer cancel()
			if err := h(ctx, ev); err != nil {
				eb.logger.Error("Event handler error",
					"subscription_id", subID,
					"module", mod,
					"event", ev.Type,
					"error", err,
				)
			}
		}(sub.ID, module, handler, event)
	}
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func (eb *EventBus) getSubscribers(eventType EventType) []*Subscription {
	if subs, ok := eb.subscribers[eventType]; ok {
		return subs
	}
	if subs, ok := eb.subscribers["*"]; ok {
		return subs
	}
	return []*Subscription{}
}

func (eb *EventBus) GetStats() map[string]interface{} {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	totalSubs := 0
	eventTypes := make(map[EventType]int)

	for eventType, subs := range eb.subscribers {
		eventTypes[eventType] = len(subs)
		totalSubs += len(subs)
	}

	return map[string]interface{}{
		"total_subscriptions": totalSubs,
		"event_types":         eventTypes,
		"queue_size":          len(eb.eventQueue),
		"queue_capacity":      eb.queueSize,
	}
}

func (eb *EventBus) Close() {
	eb.logger.Info("Shutting down event bus")
	eb.cancel()
	eb.wg.Wait()
	close(eb.eventQueue)
	eb.logger.Info("Event bus closed")
}

// ============================================================================
// SDK COMPATIBILITY - TAMBAHAN DARI eventbus_impl.go
// ============================================================================

// PublishString adalah helper untuk publish event dengan string type
func (eb *EventBus) PublishString(ctx context.Context, eventType string, source string, data map[string]interface{}) error {
	return eb.Publish(ctx, Event{
		Type:   EventType(eventType),
		Source: source,
		Data:   data,
	})
}

// SubscribeString adalah helper untuk subscribe dengan string type
func (eb *EventBus) SubscribeString(module string, eventType string, handler func(ctx context.Context, event Event) error) string {
	return eb.Subscribe(module, EventType(eventType), handler)
}