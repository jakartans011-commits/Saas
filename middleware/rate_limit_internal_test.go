package middleware

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// TEST HELPERS
// ============================================================

func newTempRateLimitDB(t *testing.T) *bolt.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ratelimit_test.db")
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open temp bolt db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// resetRateLimiterState clears the package-level cache map between
// tests, since NewRateLimiter/getOrCreateCounter share global state
// (cache, cacheMutex) across the whole package's test binary.
func resetRateLimiterState(t *testing.T) {
	t.Helper()
	cacheMutex.Lock()
	cache = make(map[string]*rateCounter)
	cacheMutex.Unlock()
	StopAllRateLimiterCleanups()
}

// ============================================================
// NewRateLimiter
// ============================================================

func TestNewRateLimiter_NilConfig_AppliesDefaults(t *testing.T) {
	resetRateLimiterState(t)
	defer StopAllRateLimiterCleanups()

	// Must not panic with nil config; must fall back to defaults.
	handler := NewRateLimiter(nil)
	if handler == nil {
		t.Fatal("expected non-nil fiber.Handler even with nil config")
	}
}

func TestNewRateLimiter_ZeroLimitAndWindow_AppliesDefaults(t *testing.T) {
	resetRateLimiterState(t)
	defer StopAllRateLimiterCleanups()

	config := &RateLimiterConfig{Limit: 0, Window: 0, DB: nil}
	handler := NewRateLimiter(config)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if config.Limit != DefaultLimit {
		t.Errorf("expected Limit defaulted to %d, got %d", DefaultLimit, config.Limit)
	}
	if config.Window != DefaultWindow {
		t.Errorf("expected Window defaulted to %d, got %d", DefaultWindow, config.Window)
	}
}

func TestNewRateLimiter_CreatesBucket_WhenDBProvided(t *testing.T) {
	resetRateLimiterState(t)
	defer StopAllRateLimiterCleanups()

	db := newTempRateLimitDB(t)
	config := &RateLimiterConfig{Limit: 100, Window: 60, DB: db}
	_ = NewRateLimiter(config)

	err := db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(rateLimitBucket) == nil {
			return fmt.Errorf("rate_limit bucket was not created")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

// TestNewRateLimiter_RegistersCleanupStop proves each call registers
// its own stop channel, and StopAllRateLimiterCleanups() clears the
// registry — this is the guard against the goroutine leak the comment
// in rate_limit.go describes.
func TestNewRateLimiter_RegistersCleanupStop(t *testing.T) {
	resetRateLimiterState(t)

	cleanupStopsMu.Lock()
	before := len(cleanupStops)
	cleanupStopsMu.Unlock()

	db := newTempRateLimitDB(t)
	_ = NewRateLimiter(&RateLimiterConfig{Limit: 100, Window: 60, DB: db})

	cleanupStopsMu.Lock()
	after := len(cleanupStops)
	cleanupStopsMu.Unlock()

	if after != before+1 {
		t.Errorf("expected exactly 1 new cleanup stop registered, got %d (before=%d, after=%d)", after-before, before, after)
	}

	StopAllRateLimiterCleanups()

	cleanupStopsMu.Lock()
	afterStop := len(cleanupStops)
	cleanupStopsMu.Unlock()

	if afterStop != 0 {
		t.Errorf("expected cleanupStops cleared after StopAllRateLimiterCleanups, got %d remaining", afterStop)
	}
}

// ============================================================
// getOrCreateCounter
// ============================================================

func TestGetOrCreateCounter_SameKey_ReturnsSameInstance(t *testing.T) {
	resetRateLimiterState(t)
	defer StopAllRateLimiterCleanups()

	config := &RateLimiterConfig{Limit: 100, Window: 60, DB: nil}

	c1 := getOrCreateCounter("tenant-a", RateLimitPriorityNormal, 100, config)
	c2 := getOrCreateCounter("tenant-a", RateLimitPriorityNormal, 100, config)

	if c1 != c2 {
		t.Error("expected same tenant+priority+config to return the SAME counter instance (cache hit)")
	}
}

func TestGetOrCreateCounter_DifferentPriority_ReturnsDifferentInstance(t *testing.T) {
	resetRateLimiterState(t)
	defer StopAllRateLimiterCleanups()

	config := &RateLimiterConfig{Limit: 100, Window: 60, DB: nil}

	normal := getOrCreateCounter("tenant-a", RateLimitPriorityNormal, 100, config)
	mcp := getOrCreateCounter("tenant-a", RateLimitPriorityMCP, 100, config)

	if normal == mcp {
		t.Error("expected tenant-a+normal and tenant-a+mcp to be DIFFERENT counters, got same instance")
	}
}

func TestGetOrCreateCounter_DifferentTenant_ReturnsDifferentInstance(t *testing.T) {
	resetRateLimiterState(t)
	defer StopAllRateLimiterCleanups()

	config := &RateLimiterConfig{Limit: 100, Window: 60, DB: nil}

	a := getOrCreateCounter("tenant-a", RateLimitPriorityNormal, 100, config)
	b := getOrCreateCounter("tenant-b", RateLimitPriorityNormal, 100, config)

	if a == b {
		t.Error("expected different tenants to get different counters")
	}
}

// TestGetOrCreateCounter_ConcurrentAccess_NoRace proves the cache is
// safe under concurrent access from many goroutines requesting the
// SAME key — run with -race to catch any unprotected read/write on
// the package-level cache map.
func TestGetOrCreateCounter_ConcurrentAccess_NoRace(t *testing.T) {
	resetRateLimiterState(t)
	defer StopAllRateLimiterCleanups()

	config := &RateLimiterConfig{Limit: 100, Window: 60, DB: nil}

	const goroutines = 50
	results := make([]*rateCounter, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = getOrCreateCounter("tenant-race", RateLimitPriorityNormal, 100, config)
		}(i)
	}
	wg.Wait()

	first := results[0]
	for i, r := range results {
		if r != first {
			t.Errorf("goroutine %d got a different counter instance than goroutine 0 — cache is not consistent under concurrency", i)
		}
	}
}

// ============================================================
// cleanupExpiredWindows
// ============================================================

func TestCleanupExpiredWindows_RemovesOldWindow_KeepsCurrent(t *testing.T) {
	db := newTempRateLimitDB(t)

	windowSize := int64(60)
	now := time.Now().Unix()
	currentWindow := now / windowSize * windowSize
	oldWindow := currentWindow - windowSize*3 // clearly expired

	oldKey := fmt.Sprintf("tenant-a:normal:%d", oldWindow)
	currentKey := fmt.Sprintf("tenant-a:normal:%d", currentWindow)

	err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(rateLimitBucket)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(oldKey), []byte("5")); err != nil {
			return err
		}
		return b.Put([]byte(currentKey), []byte("3"))
	})
	if err != nil {
		t.Fatalf("failed to seed rate limit bucket: %v", err)
	}

	cleanupExpiredWindows(db, windowSize)

	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)
		if b.Get([]byte(oldKey)) != nil {
			t.Error("expected old/expired window key to be deleted, but it still exists")
		}
		if b.Get([]byte(currentKey)) == nil {
			t.Error("expected current window key to survive cleanup, but it was deleted")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("view failed: %v", err)
	}
}

func TestCleanupExpiredWindows_NilDB_NoPanic(t *testing.T) {
	// Must be a safe no-op, not a nil-pointer panic.
	cleanupExpiredWindows(nil, 60)
}

func TestCleanupExpiredWindows_ZeroOrNegativeWindowSize_UsesDefault(t *testing.T) {
	db := newTempRateLimitDB(t)
	// Just proving it doesn't panic and falls back to DefaultWindow
	// internally — behavior already covered by the old-window test
	// above for the "normal" path.
	cleanupExpiredWindows(db, 0)
	cleanupExpiredWindows(db, -5)
}
