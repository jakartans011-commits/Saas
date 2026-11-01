package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	bolt "go.etcd.io/bbolt"
)

// TestHealthz_NoAuthRequired — /healthz harus menjawab 200 TANPA token.
//
// Pemantau eksternal (UptimeRobot/BetterStack) tidak punya kredensial.
// Kalau endpoint ini pernah berpindah ke belakang requireAuth, pemantau
// akan melaporkan 401 sebagai "down" dan Anda akan belajar mengabaikan
// alertnya — kegagalan paling berbahaya dari sebuah sistem pemantauan.
func TestHealthz_NoAuthRequired(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]any
	status := env.doJSON(t, fiber.MethodGet, "/healthz", nil, nil, &out)

	if status != fiber.StatusOK {
		t.Fatalf("/healthz tanpa auth harus 200, got %d", status)
	}
	if out["status"] != "ok" {
		t.Fatalf(`/healthz harus memulangkan status "ok", got %v`, out["status"])
	}
}

// TestReadyz_ReportsPerComponent — /readyz harus melaporkan tiap
// penyimpanan secara terpisah, bukan hanya satu kata "ready".
func TestReadyz_ReportsPerComponent(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]any
	status := env.doJSON(t, fiber.MethodGet, "/readyz", nil, nil, &out)

	if status != fiber.StatusOK {
		t.Fatalf("/readyz dengan bbolt sehat harus 200, got %d (body: %v)", status, out)
	}
	if out["status"] != "ready" {
		t.Fatalf(`/readyz harus memulangkan status "ready", got %v`, out["status"])
	}

	checks, ok := out["checks"].(map[string]any)
	if !ok {
		t.Fatalf("/readyz harus memulangkan objek checks, got %T", out["checks"])
	}
	if checks["bolt"] != "ok" {
		t.Fatalf(`checks.bolt harus "ok", got %v`, checks["bolt"])
	}
	// postgres bisa "ok" (TRAILSEAL_TEST_POSTGRES_URL diset) atau
	// "not_configured" (tidak diset). Keduanya sah; yang TIDAK sah
	// adalah field-nya hilang sama sekali.
	if _, present := checks["postgres"]; !present {
		t.Fatalf("checks.postgres harus selalu dilaporkan, got %v", checks)
	}
}

// TestReadyz_FailsWhenBoltClosed membuktikan pemeriksaan ini BISA GAGAL.
//
// Ini bagian terpenting dari berkas test ini. Sebuah health check yang
// selalu menjawab 200 lebih berbahaya daripada tidak ada health check:
// ia memberi keyakinan palsu, dan pemantau eksternal yang memantaunya
// tidak akan pernah berbunyi. Dua test rate limiter di repo ini
// (TestRateLimiter_Returns429 dan _RetryAfterHeader) punya penyakit itu.
//
// Dengan menutup bbolt lalu menembak endpoint yang sama, kita
// membuktikan 503 benar-benar bisa terjadi.
func TestReadyz_FailsWhenBoltClosed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "readyz-closed.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{NoSync: true})
	if err != nil {
		t.Fatalf("bolt.Open gagal: %v", err)
	}

	app := fiber.New()
	app.Get("/readyz", ReadyzHandler(db, nil))

	// Sehat dulu — kalau bagian ini sudah gagal, test di bawahnya tidak
	// membuktikan apa-apa.
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/readyz", nil))
	if err != nil {
		t.Fatalf("app.Test gagal: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("bbolt terbuka harus 200, got %d", resp.StatusCode)
	}

	// Sekarang tutup bbolt dan ulangi.
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close gagal: %v", err)
	}

	resp, err = app.Test(httptest.NewRequest(fiber.MethodGet, "/readyz", nil))
	if err != nil {
		t.Fatalf("app.Test gagal setelah Close: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("bbolt tertutup harus 503, got %d", resp.StatusCode)
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("body 503 bukan JSON: %v", err)
	}
	if out["status"] != "not_ready" {
		t.Fatalf(`status harus "not_ready", got %v`, out["status"])
	}

	checks, ok := out["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks hilang dari body 503: %v", out)
	}
	boltMsg, _ := checks["bolt"].(string)
	if boltMsg == "ok" || boltMsg == "" {
		t.Fatalf("checks.bolt harus melaporkan error, got %q", boltMsg)
	}
}

// TestReadyz_NilBoltIsNotReady — db nil berarti tidak ada tempat
// menyimpan tenant, user, role, maupun API key. Melaporkan "ready"
// dalam keadaan itu adalah berbohong kepada pemantau.
func TestReadyz_NilBoltIsNotReady(t *testing.T) {
	app := fiber.New()
	app.Get("/readyz", ReadyzHandler(nil, nil))

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/readyz", nil))
	if err != nil {
		t.Fatalf("app.Test gagal: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("bbolt nil harus 503, got %d", resp.StatusCode)
	}
}

// TestHealthz_StaysUpWhenNotReady — pembeda liveness dari readiness.
//
// Saat bbolt mati, /readyz HARUS gagal tapi /healthz HARUS tetap 200.
// Kalau keduanya gagal bersamaan, platform hosting akan me-restart proses
// setiap kali database bermasalah — mengubah satu gangguan jadi restart
// loop, dan menghapus log yang Anda butuhkan untuk mendiagnosisnya.
func TestHealthz_StaysUpWhenNotReady(t *testing.T) {
	app := fiber.New()
	app.Get("/healthz", HealthzHandler())
	app.Get("/readyz", ReadyzHandler(nil, nil))

	ready, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/readyz", nil))
	if err != nil {
		t.Fatalf("app.Test /readyz gagal: %v", err)
	}
	if ready.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("prasyarat test: /readyz harus 503, got %d", ready.StatusCode)
	}

	live, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/healthz", nil))
	if err != nil {
		t.Fatalf("app.Test /healthz gagal: %v", err)
	}
	if live.StatusCode != fiber.StatusOK {
		t.Fatalf("/healthz harus tetap 200 saat /readyz 503, got %d", live.StatusCode)
	}
}
