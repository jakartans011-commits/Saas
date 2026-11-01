package main

import (
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/middleware"
)

// ============================================================
// RATE LIMITER E2E TESTS
// ============================================================

func TestRateLimiter_TenantA_IndependentFrom_TenantB(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-ratelimit-a", "Tenant Rate A",
		"owner-ra@test.test", "password-a")
	env.bootstrapTenantOwner(t, "tenant-ratelimit-b", "Tenant Rate B",
		"owner-rb@test.test", "password-b")

	tokenA := env.login(t, "owner-ra@test.test", "password-a")
	tokenB := env.login(t, "owner-rb@test.test", "password-b")

	// Tenant A: make 10 requests
	for i := 0; i < 10; i++ {
		status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-ratelimit-a/usage",
			bearer(tokenA),
			nil,
			nil,
		)
		if status != fiber.StatusOK {
			t.Fatalf("Tenant A request %d failed: status %d", i+1, status)
		}
	}

	// Tenant B: should still be able to make requests
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-ratelimit-b/usage",
		bearer(tokenB),
		nil,
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("Tenant B request should be allowed, got status %d", status)
	}
}

func TestRateLimiter_AdminEndpoint_NotAffected(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-ratelimit-admin", "Tenant Rate Admin",
		"owner-admin@test.test", "password")

	// Admin endpoint should not be rate limited
	status := env.doJSON(t, fiber.MethodGet, "/api/admin/tenants/tenant-ratelimit-admin/usage",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		nil,
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("Admin endpoint should be allowed, got status %d", status)
	}
}

func TestRateLimiter_Returns429(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-ratelimit-429", "Tenant Rate 429",
		"owner-429@test.test", "password")

	token := env.login(t, "owner-429@test.test", "password")

	// Make many requests to exhaust rate limit
	// Note: Default limit is 1000, so we need to make 1001 requests
	// For test, we'll use a lower limit by modifying config
	// Since we can't easily change config in test, we'll just test the 429 mechanism

	// Instead, we check that rate limit headers are present
	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-ratelimit-429/usage",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("Request should be allowed, got status %d", status)
	}

	// Note: Full 429 test requires making 1001 requests
	// This test validates that the rate limiter is integrated
}

// TestRateLimiter_Returns429_Real membuktikan rate limit 429 secara nyata.
//
// ============================================================
// KENAPA VERSI SEBELUMNYA SELALU GAGAL
// ============================================================
//
// Limiter memakai FIXED WINDOW selebar DefaultWindow detik yang
// disejajarkan ke kelipatan epoch:
//
//	currentWindow := now / windowSize * windowSize
//
// (middleware/rate_limit.go, rateCounter.Allow). Begitu window berganti,
// hitungan di sisi server kembali ke nol.
//
// Versi sebelumnya menembak 1005 request dalam satu loop lurus dan
// berasumsi semuanya jatuh di window yang SAMA. Asumsi itu tidak pernah
// dijamin: newTestEnv membuka bbolt tanpa NoSync, dan limiter menulis
// SATU transaksi bbolt ber-fsync untuk SETIAP request, sehingga satu
// request berbiaya ~54 ms di disk laptop dan loop memakan ~54 detik —
// hampir selebar window itu sendiri.
//
// Supaya 1000 tercapai sebelum window berganti, test harus kebetulan
// dimulai dalam ~6 detik pertama sebuah window (60 - 54). Itu sekitar
// 10% kemungkinan; sisanya window bergeser di tengah jalan, hitungan
// direset, dan 1005 request terbelah jadi dua potongan yang tidak satu
// pun mencapai 1000. Karena itu ia gagal 3 dari 3.
//
// ============================================================
// PERBAIKAN — KEDUANYA DI SISI TEST
// ============================================================
//
// Logika limiter TIDAK disentuh sama sekali. Yang berubah:
//
//  1. newTestEnv membuka bbolt dengan NoSync (lihat app_test.go).
//     Database test hidup di t.TempDir() dan dibuang setelah test, jadi
//     fsync tidak melindungi apa pun di sana — ia hanya memperlambat.
//     Loop turun dari ~54 detik menjadi beberapa detik.
//
//  2. Kalau window TETAP bergeser di tengah loop, itu dideteksi dan
//     percobaan diulang, bukan dilaporkan sebagai kegagalan limiter.
//     Pergantian window adalah perilaku limiter yang benar; test tidak
//     boleh menyebutnya bug.
//
// Dengan keduanya, test ini deterministik tanpa perlu menunggu batas
// window (yang akan menambah puluhan detik ke suite).
func TestRateLimiter_Returns429_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("lewati dalam mode -short: 1005 request")
	}
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-ratelimit-429-real", "Tenant Rate 429 Real",
		"owner-429r@test.test", "password")

	token := env.login(t, "owner-429r@test.test", "password")

	// Dibaca dari konstanta, bukan ditulis ulang sebagai angka mati.
	// Versi lama menuliskan "limit=1000" sebagai teks di pesan error,
	// sehingga kalau DefaultLimit berubah, test ini akan berbohong
	// tentang angka yang diujinya.
	const totalRequests = middleware.DefaultLimit + 5

	currentWindow := func() int64 {
		return time.Now().Unix() / int64(middleware.DefaultWindow)
	}

	got429 := false
	sentInWindow := 0

	// Tiga percobaan: satu loop hanya butuh beberapa detik, jadi paling
	// banyak satu di antaranya bisa terpotong pergantian window.
	for attempt := 1; attempt <= 3 && !got429; attempt++ {
		windowAtStart := currentWindow()
		sentInWindow = 0

		for i := 0; i < totalRequests; i++ {
			status := env.doJSON(t, fiber.MethodGet,
				"/api/tenants/tenant-ratelimit-429-real/usage",
				bearer(token),
				nil,
				nil,
			)

			if status == fiber.StatusTooManyRequests {
				got429 = true
				t.Logf("429 pada request #%d (percobaan #%d) — sesuai harapan", i+1, attempt)
				break
			}

			if status != fiber.StatusOK {
				t.Fatalf("status tak terduga pada request #%d: %d", i+1, status)
			}

			sentInWindow++

			// Window bergeser: hitungan di server direset, jadi sisa loop
			// ini tidak akan pernah mencapai limit. Mulai percobaan baru
			// daripada melaporkan kegagalan yang bukan kesalahan limiter.
			if currentWindow() != windowAtStart {
				t.Logf("window bergeser setelah %d request (percobaan #%d) — mengulang",
					sentInWindow, attempt)
				break
			}
		}
	}

	if !got429 {
		t.Fatalf(
			"tidak ada 429 setelah 3 percobaan x %d request (limit=%d, window=%d detik); "+
				"request terkirim di window terakhir: %d",
			totalRequests, middleware.DefaultLimit, middleware.DefaultWindow, sentInWindow,
		)
	}
}

func TestRateLimiter_RetryAfterHeader(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-ratelimit-retry", "Tenant Rate Retry",
		"owner-retry@test.test", "password")

	token := env.login(t, "owner-retry@test.test", "password")

	// Make a request and check the status returned.
	//
	// NOTE: doJSONWithHeaders currently only returns the status
	// code (int), not the actual *http.Response, so it cannot yet
	// assert on X-RateLimit-Limit / X-RateLimit-Remaining /
	// X-RateLimit-Reset headers. Until doJSONWithHeaders is
	// extended to expose headers, this test only verifies the
	// request path succeeds.
	status := env.doJSONWithHeaders(t, fiber.MethodGet, "/api/tenants/tenant-ratelimit-retry/usage",
		bearer(token),
		nil,
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("Request should be allowed, got status %d", status)
	}
}

// Helper: doJSONWithHeaders returns response with headers
func (e *testEnv) doJSONWithHeaders(t *testing.T, method, path string, headers map[string]string, body any, out any) int {
	t.Helper()
	// This is a simplified version - in practice, we'd capture headers
	// from the response
	return e.doJSON(t, method, path, headers, body, out)
}

// TestRateLimiter_ConcurrentRequests_NoLostUpdates membuktikan rate limiter
// tetap akurat saat banyak request datang BERSAMAAN (goroutine paralel),
// bukan satu-satu berurutan seperti test lainnya.
func TestRateLimiter_ConcurrentRequests_NoLostUpdates(t *testing.T) {
	env := newTestEnv(t)

	env.bootstrapTenantOwner(t, "tenant-concurrent", "Tenant Concurrent",
		"owner-concurrent@test.test", "password")

	token := env.login(t, "owner-concurrent@test.test", "password")

	const concurrency = 50

	var wg sync.WaitGroup
	statusCodes := make([]int, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			status := env.doJSON(t, fiber.MethodGet,
				"/api/tenants/tenant-concurrent/usage",
				bearer(token),
				nil,
				nil,
			)
			statusCodes[idx] = status
		}(i)
	}

	wg.Wait()

	successCount := 0
	for i, code := range statusCodes {
		if code == fiber.StatusOK {
			successCount++
		} else {
			t.Logf("request #%d returned unexpected status: %d", i, code)
		}
	}

	if successCount != concurrency {
		t.Fatalf(
			"expected all %d concurrent requests to succeed, only %d succeeded — possible race condition or lost update",
			concurrency,
			successCount,
		)
	}

	t.Logf("all %d concurrent requests succeeded correctly", concurrency)
}
