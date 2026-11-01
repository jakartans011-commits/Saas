package middleware

import (
	"errors"
	"sync"
	"testing"
)

// TestRateLimiterStorageFailure_IsReported membuktikan kegagalan
// penyimpanan tidak pernah lewat tanpa suara.
//
// Sebelum ini, cabang fail-open hanya `return c.Next()` — tidak ada log,
// tidak ada alert. Rate limiting bisa mati berhari-hari tanpa seorang pun
// tahu, dan satu-satunya gejalanya adalah tagihan yang membengkak.
func TestRateLimiterStorageFailure_IsReported(t *testing.T) {
	var mu sync.Mutex
	var got []error

	original := rateLimiterStorageFailureHook
	rateLimiterStorageFailureHook = func(err error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, err)
	}
	t.Cleanup(func() { rateLimiterStorageFailureHook = original })

	boom := errors.New("bolt: database not open")
	notifyRateLimiterStorageFailure(boom)

	mu.Lock()
	defer mu.Unlock()

	if len(got) != 1 {
		t.Fatalf("kegagalan penyimpanan harus dilaporkan sekali, got %d", len(got))
	}
	if !errors.Is(got[0], boom) {
		t.Fatalf("error yang dilaporkan salah: %v", got[0])
	}
}

// TestRateLimiterStorageFailure_NilIsIgnored — nil bukan kegagalan.
// Melaporkannya akan membuat alert berbunyi di setiap request yang sehat.
func TestRateLimiterStorageFailure_NilIsIgnored(t *testing.T) {
	called := false
	original := rateLimiterStorageFailureHook
	rateLimiterStorageFailureHook = func(error) { called = true }
	t.Cleanup(func() { rateLimiterStorageFailureHook = original })

	notifyRateLimiterStorageFailure(nil)

	if called {
		t.Fatal("err nil tidak boleh memicu alert")
	}
}

// TestDefaultMCPLimit_FailsClosedToFreePlan — nilai bawaan MCP tidak
// boleh lebih longgar dari plan termurah.
//
// Tabel pricing: Free 60, Pro 300, Business 1000. Sebelum perbaikan,
// DefaultMCPLimit = 100 — lebih longgar dari Free. Nilai itu hanya
// terpakai bila MCPLimitResolver mengembalikan 0, yang saat ini tidak
// pernah terjadi karena resolver di app.go selalu jatuh ke Free. Tapi
// nilai bawaan yang lebih longgar dari plan termurah adalah ranjau yang
// menunggu refactor berikutnya menginjaknya.
func TestDefaultMCPLimit_FailsClosedToFreePlan(t *testing.T) {
	const freePlanMCPLimit = 60 // tabel pricing, sumber kebenaran

	if DefaultMCPLimit > freePlanMCPLimit {
		t.Fatalf(
			"DefaultMCPLimit = %d, lebih longgar dari plan Free (%d). "+
				"Nilai bawaan harus gagal-TERTUTUP ke plan termurah.",
			DefaultMCPLimit, freePlanMCPLimit,
		)
	}
}

// TestLimitForTenant_UnknownPlanNeverExceedsFree — resolver yang
// mengembalikan 0 (plan tidak dikenal) harus jatuh ke nilai bawaan, dan
// nilai bawaan itu tidak boleh melampaui Free.
func TestLimitForTenant_UnknownPlanNeverExceedsFree(t *testing.T) {
	const freePlanMCPLimit = 60

	cfg := &RateLimiterConfig{
		Limit:  DefaultLimit,
		Window: DefaultWindow,
		MCPLimitResolver: func(string) uint64 {
			return 0 // plan tidak dikenal
		},
	}

	got := limitForTenant(cfg, RateLimitPriorityMCP, "tenant-tak-dikenal")
	if got > freePlanMCPLimit {
		t.Fatalf(
			"tenant dengan plan tidak dikenal mendapat %d req/menit, "+
				"lebih dari Free (%d)", got, freePlanMCPLimit,
		)
	}
}
