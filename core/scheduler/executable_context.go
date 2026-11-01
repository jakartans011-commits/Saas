// core/scheduler/executable_context.go
package scheduler

import (
	"sync"
	"time"

	"trailseal/sdk"
)

// executableContext adalah implementasi sdk.ExecutableContext
type executableContext struct {
	mu           sync.RWMutex
	id           string
	pluginName   string
	triggerType  string
	deadline     time.Time
	values       map[string]interface{}
	cancelled    bool
	cancelReason string
}

// NewExecutableContext membuat executable context baru
func NewExecutableContext(id, pluginName, triggerType string, deadline time.Time) sdk.ExecutableContext {
	return &executableContext{
		id:          id,
		pluginName:  pluginName,
		triggerType: triggerType,
		deadline:    deadline,
		values:      make(map[string]interface{}),
		cancelled:   false,
	}
}

// ID mengembalikan ID executable
func (c *executableContext) ID() string {
	return c.id
}

// PluginName mengembalikan nama plugin
func (c *executableContext) PluginName() string {
	return c.pluginName
}

// TriggerType mengembalikan jenis trigger
func (c *executableContext) TriggerType() string {
	return c.triggerType
}

// Deadline mengembalikan deadline
func (c *executableContext) Deadline() time.Time {
	return c.deadline
}

// SetValue menyimpan nilai ke context
func (c *executableContext) SetValue(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

// GetValue mengambil nilai dari context
func (c *executableContext) GetValue(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.values[key]
}

// IsCancelled mengecek apakah dibatalkan
func (c *executableContext) IsCancelled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cancelled
}

// Cancel membatalkan executable
func (c *executableContext) Cancel(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelled = true
	c.cancelReason = reason
}
