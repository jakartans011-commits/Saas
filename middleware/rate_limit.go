package middleware

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	bolt "go.etcd.io/bbolt"
)

// ============================================================
// RATE LIMITER — Tenant-based fixed window
//
// Priority Rate Limit v1
//
// Normal:
//   1000 requests / 60 seconds / tenant
//
// MCP:
//   100 requests / 60 seconds / tenant
//
// Storage:
//   BoltDB
//
// Bucket:
//   rate_limits
//
// Key:
//   <tenantID>:<priority>:<window_start>
//
// Example:
//   tenant-a:normal:1720000000
//   tenant-a:mcp:1720000000
//
// Atomicity:
//   Read + increment + write dilakukan dalam satu BoltDB
//   write transaction untuk mencegah lost update.
//
// Burst:
//   Tidak ada.
// ============================================================

const (
	// DefaultLimit adalah jumlah request maksimum per tenant
	// untuk traffic normal dalam satu window.
	DefaultLimit = 1000

	// DefaultMCPLimit adalah jumlah request maksimum per tenant
	// untuk traffic MCP dalam satu window.
	DefaultMCPLimit = 100

	// DefaultWindow adalah ukuran fixed window dalam detik.
	DefaultWindow = 60

	// CleanupInterval menentukan seberapa sering expired
	// rate-limit windows dibersihkan.
	CleanupInterval = 10 * time.Minute
)

type RateLimitPriority string

const (
	RateLimitPriorityNormal RateLimitPriority = "normal"
	RateLimitPriorityMCP    RateLimitPriority = "mcp"
)

var (
	rateLimitBucket = []byte("rate_limits")

	cache      = make(map[string]*rateCounter)
	cacheMutex sync.RWMutex
)

// ============================================================
// CLEANUP GOROUTINE LIFECYCLE
//
// FIX (goroutine leak): setiap panggilan NewRateLimiter() men-
// start satu goroutine startCleanup().
//
// Di production BuildApp biasanya hanya dibuat sekali.
//
// Di test, BuildApp/NewRateLimiter dapat dipanggil berulang.
// Karena itu setiap stop-channel didaftarkan ke registry
// package-level dan test harness dapat memanggil
// StopAllRateLimiterCleanups().
// ============================================================

var (
	cleanupStops   []chan struct{}
	cleanupStopsMu sync.Mutex
)

// StopAllRateLimiterCleanups menghentikan SEMUA goroutine
// startCleanup yang sedang berjalan.
//
// HANYA untuk dipakai di test.
//
// Jangan dipanggil di production/main().
func StopAllRateLimiterCleanups() {
	cleanupStopsMu.Lock()
	defer cleanupStopsMu.Unlock()

	for _, stop := range cleanupStops {
		close(stop)
	}

	cleanupStops = nil
}

func registerCleanupStop(stop chan struct{}) {
	cleanupStopsMu.Lock()
	defer cleanupStopsMu.Unlock()

	cleanupStops = append(cleanupStops, stop)
}

// ============================================================
// CONFIG
// ============================================================

type RateLimiterConfig struct {
	Limit  uint64
	Window int64
	DB     *bolt.DB

	// MCPLimitResolver (opsional) mengembalikan limit MCP per window untuk
	// satu tenant. Nil atau hasil 0 memakai DefaultMCPLimit.
	MCPLimitResolver func(tenantID string) uint64
}

func DefaultRateLimiterConfig(db *bolt.DB) *RateLimiterConfig {
	return &RateLimiterConfig{
		Limit:  DefaultLimit,
		Window: DefaultWindow,
		DB:     db,
	}
}

// ============================================================
// PRIORITY RESOLUTION
// ============================================================
//
// Priority v1 sengaja ditentukan dari request path.
//
// Normal:
//   /api/tenants/:id/...
//
// MCP:
//   /api/tenants/:id/mcp/...
//
// Dengan pendekatan ini app.go tidak perlu mengubah signature
// NewRateLimiter() dan route MCP yang sudah ada otomatis
// mendapatkan quota MCP.
// ============================================================

func resolveRateLimitPriority(path string) RateLimitPriority {
	path = strings.ToLower(path)

	if path == "/mcp" ||
		strings.HasPrefix(path, "/mcp/") ||
		strings.Contains(path, "/mcp/") {
		return RateLimitPriorityMCP
	}

	return RateLimitPriorityNormal
}

func limitForPriority(
	config *RateLimiterConfig,
	priority RateLimitPriority,
) uint64 {
	if priority == RateLimitPriorityMCP {
		return DefaultMCPLimit
	}

	if config.Limit == 0 {
		return DefaultLimit
	}

	return config.Limit
}

// ============================================================
// COUNTER
// ============================================================

type rateCounter struct {
	mu sync.Mutex

	tenantID string
	priority RateLimitPriority

	db *bolt.DB

	// Current fixed window start timestamp.
	window int64

	// Local mirror of the current DB counter.
	count uint64

	// Rate limit configuration.
	limit      uint64
	windowSize int64

	lastClean time.Time
}

// ============================================================
// NEW RATE LIMITER
// ============================================================
//
// Backward-compatible:
//
//   NewRateLimiter(config)
//
// tetap menjadi API utama.
//
// Priority dipilih otomatis berdasarkan request path.
// ============================================================

func NewRateLimiter(config *RateLimiterConfig) fiber.Handler {
	if config == nil {
		config = DefaultRateLimiterConfig(nil)
	}

	if config.Limit == 0 {
		config.Limit = DefaultLimit
	}

	if config.Window <= 0 {
		config.Window = DefaultWindow
	}

	if config.DB != nil {
		if err := config.DB.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(rateLimitBucket)
			return err
		}); err != nil {
			panic(fmt.Sprintf(
				"failed to init rate_limit bucket: %v",
				err,
			))
		}
	}

	stop := make(chan struct{})

	registerCleanupStop(stop)

	go startCleanup(
		config.DB,
		config.Window,
		stop,
	)

	return func(c *fiber.Ctx) error {
		if config.DB == nil {
			return c.Next()
		}

		var tenantID string
		if claims, ok := ClaimsFromFiberCtx(c); ok {
			tenantID = claims.TenantID
		} else if key, ok := APIKeyFromFiberCtx(c); ok && key != nil {
			// Jalur API key (mis. /mcp/v1/rpc): tenant diambil dari key.
			tenantID = key.TenantID
		} else {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		if tenantID == "" {
			return c.Next()
		}

		// Admin/root endpoints intentionally bypass the tenant
		// rate limiter.
		path := c.Path()

		if len(path) >= len("/api/admin/") &&
			path[:len("/api/admin/")] == "/api/admin/" {
			return c.Next()
		}

		priority := resolveRateLimitPriority(path)

		limit := limitForTenant(
			config,
			priority,
			tenantID,
		)

		counter := getOrCreateCounter(
			tenantID,
			priority,
			limit,
			config,
		)

		allowed, remaining, resetAt, err := counter.Allow()

		if err != nil {
			// Fail-open:
			// jika storage rate limiter gagal, jangan membuat
			// seluruh API unavailable.
			return c.Next()
		}

		c.Set(
			"X-RateLimit-Limit",
			strconv.FormatUint(limit, 10),
		)

		c.Set(
			"X-RateLimit-Remaining",
			strconv.FormatUint(remaining, 10),
		)

		c.Set(
			"X-RateLimit-Reset",
			strconv.FormatInt(resetAt, 10),
		)

		if !allowed {
			retryAfter := resetAt - time.Now().Unix()

			if retryAfter < 1 {
				retryAfter = 1
			}

			c.Set(
				"Retry-After",
				strconv.FormatInt(retryAfter, 10),
			)

			return c.Status(
				fiber.StatusTooManyRequests,
			).JSON(fiber.Map{
				"error":    "rate_limit_exceeded",
				"limit":    limit,
				"priority": string(priority),
				"reset":    resetAt,
			})
		}

		return c.Next()
	}
}

// limitForTenant = limitForPriority + override per plan untuk traffic MCP.
func limitForTenant(
	config *RateLimiterConfig,
	priority RateLimitPriority,
	tenantID string,
) uint64 {
	limit := limitForPriority(config, priority)

	if priority == RateLimitPriorityMCP && config.MCPLimitResolver != nil {
		if l := config.MCPLimitResolver(tenantID); l > 0 {
			return l
		}
	}

	return limit
}

// ============================================================
// COUNTER CACHE
// ============================================================

func getOrCreateCounter(
	tenantID string,
	priority RateLimitPriority,
	limit uint64,
	config *RateLimiterConfig,
) *rateCounter {
	// Include:
	//
	//   DB
	//   limit
	//   window
	//   tenant
	//   priority
	//
	// dalam cache identity.
	//
	// Ini memastikan:
	//
	// tenant-a + normal
	//
	// dan
	//
	// tenant-a + mcp
	//
	// memiliki counter yang berbeda.

	cacheKey := fmt.Sprintf(
		"%p:%d:%d:%s:%s",
		config.DB,
		limit,
		config.Window,
		tenantID,
		priority,
	)

	cacheMutex.RLock()

	counter, exists := cache[cacheKey]

	cacheMutex.RUnlock()

	if exists {
		return counter
	}

	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if counter, exists := cache[cacheKey]; exists {
		return counter
	}

	now := time.Now().Unix()

	currentWindow :=
		now / config.Window *
			config.Window

	counter = &rateCounter{
		tenantID:   tenantID,
		priority:   priority,
		db:         config.DB,
		window:     currentWindow,
		count:      0,
		limit:      limit,
		windowSize: config.Window,
		lastClean:  time.Now(),
	}

	cache[cacheKey] = counter

	return counter
}

// ============================================================
// ALLOW
// ============================================================
//
// Seluruh operasi:
//
//   read current count
//   check limit
//   increment
//   write new count
//
// dilakukan dalam SATU BoltDB write transaction.
//
// Ini mencegah:
//
// Request A membaca 10
// Request B membaca 10
// Request A menulis 11
// Request B menulis 11
//
// yang akan menyebabkan satu request "hilang" dari counter.
// ============================================================

func (r *rateCounter) Allow() (
	allowed bool,
	remaining uint64,
	resetAt int64,
	err error,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.windowSize <= 0 {
		r.windowSize = DefaultWindow
	}

	if r.limit == 0 {
		r.limit = DefaultLimit
	}

	now := time.Now().Unix()

	currentWindow :=
		now / r.windowSize *
			r.windowSize

	// Window berubah.
	if currentWindow != r.window {
		r.window = currentWindow
		r.count = 0
	}

	resetAt =
		r.window +
			r.windowSize

	err = r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)

		if b == nil {
			return fmt.Errorf(
				"rate_limit bucket not found",
			)
		}

		// Priority-aware key:
		//
		// <tenant>:<priority>:<window>
		key := []byte(
			r.tenantID +
				":" +
				string(r.priority) +
				":" +
				strconv.FormatInt(
					r.window,
					10,
				),
		)

		var currentCount uint64

		data := b.Get(key)

		if data != nil {
			if len(data) != 8 {
				return fmt.Errorf(
					"invalid rate limit counter size",
				)
			}

			currentCount =
				binary.BigEndian.Uint64(data)
		}

		// Limit sudah tercapai.
		if currentCount >= r.limit {
			r.count = currentCount

			allowed = false
			remaining = 0

			return nil
		}

		// Increment dilakukan di transaction yang sama.
		currentCount++

		data = make([]byte, 8)

		binary.BigEndian.PutUint64(
			data,
			currentCount,
		)

		if err := b.Put(key, data); err != nil {
			return err
		}

		r.count = currentCount

		allowed = true
		remaining =
			r.limit -
				currentCount

		return nil
	})

	if err != nil {
		return false, 0, resetAt, err
	}

	return allowed, remaining, resetAt, nil
}

// ============================================================
// CLEANUP
// ============================================================

func startCleanup(
	db *bolt.DB,
	windowSize int64,
	stop chan struct{},
) {
	if db == nil {
		return
	}

	if windowSize <= 0 {
		windowSize = DefaultWindow
	}

	ticker := time.NewTicker(CleanupInterval)

	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cleanupExpiredWindows(
				db,
				windowSize,
			)

		case <-stop:
			return
		}
	}
}

// ============================================================
// CLEANUP EXPIRED WINDOWS
// ============================================================
//
// Current key format:
//
//   tenantID:priority:windowStart
//
// Contoh:
//
//   tenant-a:normal:1720000000
//   tenant-a:mcp:1720000000
//
// Kita menggunakan colon terakhir untuk mengambil windowStart.
// Jadi tenant ID tetap dapat mengandung colon.
// ============================================================

func cleanupExpiredWindows(
	db *bolt.DB,
	windowSize int64,
) {
	if db == nil {
		return
	}

	if windowSize <= 0 {
		windowSize = DefaultWindow
	}

	now := time.Now().Unix()

	currentWindow :=
		now / windowSize *
			windowSize

	expiredBefore :=
		currentWindow -
			windowSize

	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rateLimitBucket)

		if b == nil {
			return nil
		}

		cursor := b.Cursor()

		var keysToDelete [][]byte

		for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
			keyStr := string(k)

			// Window selalu berada setelah colon terakhir.
			lastColon := -1

			for i := len(keyStr) - 1; i >= 0; i-- {
				if keyStr[i] == ':' {
					lastColon = i
					break
				}
			}

			if lastColon == -1 ||
				lastColon+1 >= len(keyStr) {
				continue
			}

			windowStr :=
				keyStr[lastColon+1:]

			windowStart, parseErr :=
				strconv.ParseInt(
					windowStr,
					10,
					64,
				)

			if parseErr != nil {
				continue
			}

			if windowStart < expiredBefore {
				keyCopy :=
					make([]byte, len(k))

				copy(
					keyCopy,
					k,
				)

				keysToDelete =
					append(
						keysToDelete,
						keyCopy,
					)
			}
		}

		for _, key := range keysToDelete {
			if err := b.Delete(key); err != nil {
				return err
			}
		}

		return nil
	})

	// Cleanup failure tidak boleh membuat API crash.
	if err != nil {
		_ = err
	}
}
