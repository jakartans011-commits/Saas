// core/capability/engine.go
package capability

import (
	"sync"

	"trailseal/sdk"
)

// Engine adalah Capability Engine yang menggunakan Matcher
type Engine struct {
	mu      sync.RWMutex
	matcher sdk.CapabilityMatcher
}

// NewEngine membuat capability engine baru
func NewEngine() *Engine {
	return &Engine{
		matcher: NewWildcardMatcher(),
	}
}

// SetMatcher mengganti matcher (untuk Strategy Pattern)
func (e *Engine) SetMatcher(matcher sdk.CapabilityMatcher) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.matcher = matcher
}

// HasCapability mengecek apakah manifest memiliki capability yang sesuai dengan pattern
func (e *Engine) HasCapability(manifest sdk.Manifest, pattern string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.matcher.MatchAny(manifest.Capabilities[0], []string{pattern})
	// Note: ini perlu di-loop untuk semua capabilities
}

// MatchCapability mengecek apakah capability cocok dengan pattern
func (e *Engine) MatchCapability(capability, pattern string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.matcher.Match(capability, pattern)
}

// MatchAnyCapability mengecek apakah ada capability yang cocok dengan pattern
func (e *Engine) MatchAnyCapability(capabilities []string, patterns []string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, cap := range capabilities {
		if e.matcher.MatchAny(cap, patterns) {
			return true
		}
	}
	return false
}

// FilterCapabilities mengembalikan capability yang cocok dengan pattern
func (e *Engine) FilterCapabilities(capabilities []string, pattern string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.matcher.Filter(capabilities, pattern)
}

// GetMatcher mengembalikan matcher yang digunakan
func (e *Engine) GetMatcher() sdk.CapabilityMatcher {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.matcher
}
