// core/health/heartbeat.go
package health

import (
	"sync"
	"time"

	"trailseal/sdk"
)

// Heartbeat adalah mekanisme untuk mendeteksi health monitor down
type Heartbeat struct {
	mu        sync.RWMutex
	logger    sdk.Logger
	lastBeat  time.Time
	isAlive   bool
	interval  time.Duration
	stopCh    chan struct{}
	isStopped bool
}

// NewHeartbeat membuat heartbeat baru
func NewHeartbeat(logger sdk.Logger) *Heartbeat {
	return &Heartbeat{
		logger:    logger,
		lastBeat:  time.Now(),
		isAlive:   true,
		interval:  10 * time.Second,
		stopCh:    make(chan struct{}),
		isStopped: false,
	}
}

// Start memulai heartbeat
func (h *Heartbeat) Start() {
	h.mu.Lock()

	if !h.isStopped {
		h.mu.Unlock()
		return
	}

	h.stopCh = make(chan struct{})
	h.isStopped = false

	stopCh := h.stopCh
	interval := h.interval

	h.mu.Unlock()

	go h.loop(stopCh, interval)

	h.logger.Info("Heartbeat started", "interval", interval)
}

// Stop menghentikan heartbeat
func (h *Heartbeat) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isStopped {
		return
	}

	h.isStopped = true
	close(h.stopCh)

	h.logger.Info("Heartbeat stopped")
}

// loop adalah loop heartbeat
func (h *Heartbeat) loop(stopCh <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			h.check()
		}
	}
}

// check memeriksa apakah heartbeat masih hidup
func (h *Heartbeat) check() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if time.Since(h.lastBeat) > h.interval*2 {
		if h.isAlive {
			h.isAlive = false
			h.logger.Warn("Heartbeat lost - health monitor may be down")
		}
	} else {
		if !h.isAlive {
			h.isAlive = true
			h.logger.Info("Heartbeat restored - health monitor recovered")
		}
	}
}

// Ping menandai heartbeat sebagai alive
func (h *Heartbeat) Ping() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastBeat = time.Now()
	h.isAlive = true
}

// Status mengembalikan status heartbeat
func (h *Heartbeat) Status() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return map[string]interface{}{
		"is_alive":    h.isAlive,
		"last_beat":   h.lastBeat,
		"seconds_ago": time.Since(h.lastBeat).Seconds(),
	}
}

// IsAlive mengembalikan apakah heartbeat masih hidup
func (h *Heartbeat) IsAlive() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.isAlive
}
