package middleware

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openRateLimitTestDB(t *testing.T, name string) *bolt.DB {
	t.Helper()

	tmpDir := t.TempDir()

	dbPath := filepath.Join(
		tmpDir,
		name,
	)

	db, err := bolt.Open(
		dbPath,
		0600,
		nil,
	)

	if err != nil {
		t.Fatalf(
			"failed to open test db: %v",
			err,
		)
	}

	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(
			rateLimitBucket,
		)

		return err
	}); err != nil {
		db.Close()

		t.Fatalf(
			"failed to init bucket: %v",
			err,
		)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// ============================================================
// BASIC ALLOW
// ============================================================

func TestRateLimiter_Allow(t *testing.T) {
	db := openRateLimitTestDB(
		t,
		"rate_test.db",
	)

	config := DefaultRateLimiterConfig(db)

	config.Limit = 5
	config.Window = 60

	counter := &rateCounter{
		tenantID: "test-tenant",
		priority: RateLimitPriorityNormal,
		db:       db,
		window: time.Now().Unix() /
			config.Window *
			config.Window,
		limit:      config.Limit,
		windowSize: config.Window,
	}

	for i := 0; i < 5; i++ {
		allowed, remaining, _, err :=
			counter.Allow()

		if err != nil {
			t.Fatalf(
				"Allow failed: %v",
				err,
			)
		}

		if !allowed {
			t.Fatalf(
				"Request %d should be allowed, got denied",
				i+1,
			)
		}

		if remaining != uint64(4-i) {
			t.Fatalf(
				"Expected remaining %d, got %d",
				4-i,
				remaining,
			)
		}
	}

	allowed, _, _, err :=
		counter.Allow()

	if err != nil {
		t.Fatalf(
			"Allow failed: %v",
			err,
		)
	}

	if allowed {
		t.Fatal(
			"6th request should be denied",
		)
	}
}

// ============================================================
// WINDOW RESET
// ============================================================

func TestRateLimiter_WindowReset(t *testing.T) {
	db := openRateLimitTestDB(
		t,
		"rate_window_test.db",
	)

	config := DefaultRateLimiterConfig(db)

	config.Limit = 5
	config.Window = 60

	now := time.Now().Unix()

	currentWindow :=
		now / config.Window *
			config.Window

	previousWindow :=
		currentWindow -
			config.Window

	counter := &rateCounter{
		tenantID:   "test-tenant",
		priority:   RateLimitPriorityNormal,
		db:         db,
		window:     previousWindow,
		count:      5,
		limit:      config.Limit,
		windowSize: config.Window,
	}

	// Simulasikan bahwa tenant sudah menghabiskan
	// seluruh limit pada window sebelumnya.
	if err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)

		if b == nil {
			return nil
		}

		key := []byte(
			"test-tenant:" +
				string(RateLimitPriorityNormal) +
				":" +
				strconv.FormatInt(
					previousWindow,
					10,
				),
		)

		data := make([]byte, 8)

		binary.BigEndian.PutUint64(
			data,
			5,
		)

		return b.Put(
			key,
			data,
		)
	}); err != nil {
		t.Fatalf(
			"failed to seed previous window: %v",
			err,
		)
	}

	allowed, remaining, resetAt, err :=
		counter.Allow()

	if err != nil {
		t.Fatalf(
			"Allow failed after window reset: %v",
			err,
		)
	}

	if !allowed {
		t.Fatal(
			"Request after window reset should be allowed",
		)
	}

	if remaining != 4 {
		t.Fatalf(
			"Expected remaining 4 after reset, got %d",
			remaining,
		)
	}

	expectedReset :=
		currentWindow +
			config.Window

	if resetAt != expectedReset {
		t.Fatalf(
			"Expected reset at %d, got %d",
			expectedReset,
			resetAt,
		)
	}

	// Pastikan request masuk ke window baru.
	var storedCount uint64

	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)

		if b == nil {
			return nil
		}

		key := []byte(
			"test-tenant:" +
				string(RateLimitPriorityNormal) +
				":" +
				strconv.FormatInt(
					currentWindow,
					10,
				),
		)

		data := b.Get(key)

		if data == nil {
			return nil
		}

		if len(data) != 8 {
			return fmt.Errorf(
				"invalid stored counter length: %d",
				len(data),
			)
		}

		storedCount =
			binary.BigEndian.Uint64(data)

		return nil
	})

	if err != nil {
		t.Fatalf(
			"failed to read current window: %v",
			err,
		)
	}

	if storedCount != 1 {
		t.Fatalf(
			"Expected current window counter 1, got %d",
			storedCount,
		)
	}
}

// ============================================================
// CLEANUP
// ============================================================

func TestRateLimiter_Cleanup(t *testing.T) {
	db := openRateLimitTestDB(
		t,
		"rate_cleanup_test.db",
	)

	now := time.Now().Unix()

	window := int64(DefaultWindow)

	currentWindow :=
		now / window *
			window

	oldWindow :=
		currentWindow -
			window*2

	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)

		if b == nil {
			return nil
		}

		data := make([]byte, 8)

		binary.BigEndian.PutUint64(
			data,
			10,
		)

		oldKey := []byte(
			"test-tenant:" +
				string(RateLimitPriorityNormal) +
				":" +
				strconv.FormatInt(
					oldWindow,
					10,
				),
		)

		if err := b.Put(
			oldKey,
			data,
		); err != nil {
			return err
		}

		currentKey := []byte(
			"test-tenant:" +
				string(RateLimitPriorityMCP) +
				":" +
				strconv.FormatInt(
					currentWindow,
					10,
				),
		)

		if err := b.Put(
			currentKey,
			data,
		); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		t.Fatalf(
			"failed to insert test data: %v",
			err,
		)
	}

	cleanupExpiredWindows(
		db,
		window,
	)

	var count int

	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)

		if b == nil {
			return nil
		}

		c := b.Cursor()

		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			count++
		}

		return nil
	})

	if err != nil {
		t.Fatalf(
			"failed to read bucket: %v",
			err,
		)
	}

	if count != 1 {
		t.Fatalf(
			"Expected 1 entry after cleanup, got %d",
			count,
		)
	}
}

// ============================================================
// PRIORITY V1
// ============================================================

// TestRateLimiter_PriorityIsolation prova bahwa:
//
//	tenant-A + normal
//
// dan
//
//	tenant-A + mcp
//
// memiliki counter yang berbeda.
//
// Ini adalah inti Priority Rate Limit v1.
func TestRateLimiter_PriorityIsolation(t *testing.T) {
	db := openRateLimitTestDB(
		t,
		"rate_priority_isolation.db",
	)

	window := int64(60)

	normal := &rateCounter{
		tenantID: "tenant-a",
		priority: RateLimitPriorityNormal,
		db:       db,
		window: time.Now().Unix() /
			window *
			window,
		limit:      2,
		windowSize: window,
	}

	mcp := &rateCounter{
		tenantID:   "tenant-a",
		priority:   RateLimitPriorityMCP,
		db:         db,
		window:     normal.window,
		limit:      2,
		windowSize: window,
	}

	// Habiskan normal quota.
	for i := 0; i < 2; i++ {
		allowed, _, _, err :=
			normal.Allow()

		if err != nil {
			t.Fatalf(
				"normal Allow failed: %v",
				err,
			)
		}

		if !allowed {
			t.Fatalf(
				"normal request %d unexpectedly denied",
				i+1,
			)
		}
	}

	// Normal sekarang harus denied.
	allowed, _, _, err :=
		normal.Allow()

	if err != nil {
		t.Fatalf(
			"normal final Allow failed: %v",
			err,
		)
	}

	if allowed {
		t.Fatal(
			"normal quota should be exhausted",
		)
	}

	// MCP harus tetap bisa request karena counter
	// MCP terpisah dari normal.
	allowed, remaining, _, err :=
		mcp.Allow()

	if err != nil {
		t.Fatalf(
			"mcp Allow failed: %v",
			err,
		)
	}

	if !allowed {
		t.Fatal(
			"MCP request should not be affected by normal quota",
		)
	}

	if remaining != 1 {
		t.Fatalf(
			"expected MCP remaining 1, got %d",
			remaining,
		)
	}
}

// ============================================================
// PRIORITY V1 — DIFFERENT LIMITS
// ============================================================

// TestRateLimiter_PriorityDifferentLimits membuktikan:
//
// Normal = 1000
// MCP    = 100
//
// secara policy.
func TestRateLimiter_PriorityDifferentLimits(t *testing.T) {
	config := DefaultRateLimiterConfig(nil)

	if config.Limit != DefaultLimit {
		t.Fatalf(
			"expected default normal limit %d, got %d",
			DefaultLimit,
			config.Limit,
		)
	}

	normalLimit :=
		limitForPriority(
			config,
			RateLimitPriorityNormal,
		)

	mcpLimit :=
		limitForPriority(
			config,
			RateLimitPriorityMCP,
		)

	if normalLimit != DefaultLimit {
		t.Fatalf(
			"expected normal limit %d, got %d",
			DefaultLimit,
			normalLimit,
		)
	}

	if mcpLimit != DefaultMCPLimit {
		t.Fatalf(
			"expected MCP limit %d, got %d",
			DefaultMCPLimit,
			mcpLimit,
		)
	}

	if normalLimit == mcpLimit {
		t.Fatal(
			"normal and MCP limits must be different",
		)
	}
}

// ============================================================
// PRIORITY V1 — PATH RESOLUTION
// ============================================================

func TestRateLimiter_ResolvePriority(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected RateLimitPriority
	}{
		{
			name:     "normal tenant route",
			path:     "/api/tenants/tenant-a/tools",
			expected: RateLimitPriorityNormal,
		},
		{
			name:     "MCP query",
			path:     "/api/tenants/tenant-a/mcp/query",
			expected: RateLimitPriorityMCP,
		},
		{
			name:     "MCP nested route",
			path:     "/api/tenants/tenant-a/mcp/tools/call",
			expected: RateLimitPriorityMCP,
		},
		{
			name:     "uppercase MCP",
			path:     "/api/tenants/tenant-a/MCP/query",
			expected: RateLimitPriorityMCP,
		},
		{
			name:     "non MCP route containing similar text",
			path:     "/api/tenants/tenant-a/tool-mcp/query",
			expected: RateLimitPriorityNormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got :=
				resolveRateLimitPriority(
					tt.path,
				)

			if got != tt.expected {
				t.Fatalf(
					"expected priority %q, got %q",
					tt.expected,
					got,
				)
			}
		})
	}
}

// ============================================================
// PRIORITY V1 — MCP LIMIT EXHAUSTION
// ============================================================

// TestRateLimiter_MCPLimitExhaustion menggunakan limit kecil
// agar test tidak perlu melakukan 100 request.
//
// Ini membuktikan mekanisme yang sama dengan production
// MCP limit, hanya dengan quota yang diperkecil.
func TestRateLimiter_MCPLimitExhaustion(t *testing.T) {
	db := openRateLimitTestDB(
		t,
		"rate_mcp_exhaustion.db",
	)

	window := int64(60)

	counter := &rateCounter{
		tenantID: "tenant-mcp",
		priority: RateLimitPriorityMCP,
		db:       db,
		window: time.Now().Unix() /
			window *
			window,
		limit:      3,
		windowSize: window,
	}

	for i := 0; i < 3; i++ {
		allowed, remaining, _, err :=
			counter.Allow()

		if err != nil {
			t.Fatalf(
				"MCP Allow failed: %v",
				err,
			)
		}

		if !allowed {
			t.Fatalf(
				"MCP request %d should be allowed",
				i+1,
			)
		}

		expectedRemaining :=
			uint64(2 - i)

		if remaining != expectedRemaining {
			t.Fatalf(
				"expected remaining %d, got %d",
				expectedRemaining,
				remaining,
			)
		}
	}

	allowed, remaining, _, err :=
		counter.Allow()

	if err != nil {
		t.Fatalf(
			"MCP final Allow failed: %v",
			err,
		)
	}

	if allowed {
		t.Fatal(
			"4th MCP request should be denied",
		)
	}

	if remaining != 0 {
		t.Fatalf(
			"expected remaining 0 after MCP quota exhausted, got %d",
			remaining,
		)
	}
}

// ============================================================
// PRIORITY V1 — TENANT ISOLATION
// ============================================================

func TestRateLimiter_PriorityTenantIsolation(t *testing.T) {
	db := openRateLimitTestDB(
		t,
		"rate_priority_tenant_isolation.db",
	)

	window := int64(60)

	tenantA := &rateCounter{
		tenantID: "tenant-a",
		priority: RateLimitPriorityMCP,
		db:       db,
		window: time.Now().Unix() /
			window *
			window,
		limit:      1,
		windowSize: window,
	}

	tenantB := &rateCounter{
		tenantID:   "tenant-b",
		priority:   RateLimitPriorityMCP,
		db:         db,
		window:     tenantA.window,
		limit:      1,
		windowSize: window,
	}

	allowed, _, _, err :=
		tenantA.Allow()

	if err != nil {
		t.Fatalf(
			"tenant A Allow failed: %v",
			err,
		)
	}

	if !allowed {
		t.Fatal(
			"tenant A first MCP request should be allowed",
		)
	}

	// Tenant A sudah habis.
	allowed, _, _, err =
		tenantA.Allow()

	if err != nil {
		t.Fatalf(
			"tenant A second Allow failed: %v",
			err,
		)
	}

	if allowed {
		t.Fatal(
			"tenant A second MCP request should be denied",
		)
	}

	// Tenant B harus tetap punya quota sendiri.
	allowed, remaining, _, err :=
		tenantB.Allow()

	if err != nil {
		t.Fatalf(
			"tenant B Allow failed: %v",
			err,
		)
	}

	if !allowed {
		t.Fatal(
			"tenant B MCP request should not be affected by tenant A",
		)
	}

	if remaining != 0 {
		t.Fatalf(
			"expected tenant B remaining 0, got %d",
			remaining,
		)
	}
}

// ============================================================
// PRIORITY V1 — DATABASE KEY SEPARATION
// ============================================================

func TestRateLimiter_PriorityDatabaseKeysAreSeparated(t *testing.T) {
	db := openRateLimitTestDB(
		t,
		"rate_priority_keys.db",
	)

	window := int64(60)

	now := time.Now().Unix()

	currentWindow :=
		now / window *
			window

	normal := &rateCounter{
		tenantID:   "tenant-a",
		priority:   RateLimitPriorityNormal,
		db:         db,
		window:     currentWindow,
		limit:      10,
		windowSize: window,
	}

	mcp := &rateCounter{
		tenantID:   "tenant-a",
		priority:   RateLimitPriorityMCP,
		db:         db,
		window:     currentWindow,
		limit:      10,
		windowSize: window,
	}

	if _, _, _, err := normal.Allow(); err != nil {
		t.Fatalf(
			"normal Allow failed: %v",
			err,
		)
	}

	if _, _, _, err := mcp.Allow(); err != nil {
		t.Fatalf(
			"MCP Allow failed: %v",
			err,
		)
	}

	var normalCount uint64
	var mcpCount uint64

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)

		if b == nil {
			return fmt.Errorf(
				"rate_limit bucket not found",
			)
		}

		normalKey := []byte(
			"tenant-a:" +
				string(RateLimitPriorityNormal) +
				":" +
				strconv.FormatInt(
					currentWindow,
					10,
				),
		)

		mcpKey := []byte(
			"tenant-a:" +
				string(RateLimitPriorityMCP) +
				":" +
				strconv.FormatInt(
					currentWindow,
					10,
				),
		)

		normalData := b.Get(normalKey)

		mcpData := b.Get(mcpKey)

		if len(normalData) != 8 {
			return fmt.Errorf(
				"invalid normal counter length: %d",
				len(normalData),
			)
		}

		if len(mcpData) != 8 {
			return fmt.Errorf(
				"invalid MCP counter length: %d",
				len(mcpData),
			)
		}

		normalCount =
			binary.BigEndian.Uint64(
				normalData,
			)

		mcpCount =
			binary.BigEndian.Uint64(
				mcpData,
			)

		return nil
	})

	if err != nil {
		t.Fatalf(
			"failed to inspect priority keys: %v",
			err,
		)
	}

	if normalCount != 1 {
		t.Fatalf(
			"expected normal counter 1, got %d",
			normalCount,
		)
	}

	if mcpCount != 1 {
		t.Fatalf(
			"expected MCP counter 1, got %d",
			mcpCount,
		)
	}
}
