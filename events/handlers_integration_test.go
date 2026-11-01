package events

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/apikey"
	"trailseal/middleware"
)

// ============================================================
// HANDLERS TESTS — Postgres sungguhan, pola sama dengan
// retention_test.go. Reuse retentionTestPool/retentionTestLogger/
// cleanupRetentionTestProject dari file itu (package sama).
//
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go test ./events -run TestHandlers -v
// ============================================================

// ============================================================
// mockAPIKeyStore lokal — mengimplementasikan middleware.APIKeyStore.
// Tidak reuse mock milik package middleware karena unexported di
// package berbeda.
// ============================================================

type eventsMockAPIKeyStore struct {
	key *apikey.APIKey
}

func (m *eventsMockAPIKeyStore) FindByPlaintext(ctx context.Context, plaintext string) (*apikey.APIKey, error) {
	if m.key == nil {
		return nil, apikey.ErrNotFound
	}
	return m.key, nil
}

func (m *eventsMockAPIKeyStore) UpdateLastUsed(ctx context.Context, keyID string) error {
	return nil
}

// newEventsTestApp membangun Fiber app dengan chain yang sama
// seperti produksi: RequireAPIKey → handler. Ini memastikan
// key.ProjectID/key.TenantID benar-benar datang dari context yang
// divalidasi middleware, bukan dari body/query (sesuai komentar
// keamanan di handlers.go).
func newEventsTestApp(store *PostgresStore, key *apikey.APIKey) *fiber.App {
	app := fiber.New()
	mockStore := &eventsMockAPIKeyStore{key: key}

	app.Use(middleware.RequireAPIKey(mockStore))
	app.Post("/v1/events", IngestHandler(store, nil, nil, nil))
	app.Post("/v1/events/batch", BatchIngestHandler(store, nil, nil, nil))
	app.Get("/v1/events", QueryHandler(store))
	app.Post("/v1/events/verify", VerifyHandler(store))
	app.Get("/v1/events/export", ExportHandler(store))

	return app
}

// createTestProjectWithLimit membuat project test dengan
// monthly_event_limit custom (dibutuhkan untuk test quota_exceeded
// dan supaya test lain tidak saling menabrak kuota default).
// Selalu dibersihkan lewat cleanupRetentionTestProject.
func createTestProjectWithLimit(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	monthlyLimit int64,
) string {
	t.Helper()

	var projectID string
	err := pool.QueryRow(ctx, `
		INSERT INTO projects (
			tenant_id, name, status, retention_days, monthly_event_limit
		)
		VALUES ($1, $2, 'active', $3, $4)
		RETURNING id
	`,
		"events-test-tenant",
		"events-test-"+time.Now().Format("20060102150405.000000000"),
		90,
		monthlyLimit,
	).Scan(&projectID)
	if err != nil {
		t.Fatalf("failed to create events test project: %v", err)
	}

	t.Cleanup(func() {
		cleanupRetentionTestProject(t, context.Background(), pool, projectID)
	})

	return projectID
}

// createTestProjectWithTenantQuota membuat project test yang terikat
// ke tenantID custom, DAN men-seed tenant_quotas untuk tenant itu.
// Dipakai oleh test yang perlu kontrol kuota per-tenant secara presisi
// (misal test quota_exceeded), supaya tidak berbagi kuota dengan
// tenant default "events-test-tenant" yang dipakai test lain.
func createTestProjectWithTenantQuota(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	monthlyLimit int64,
) string {
	t.Helper()

	// Seed tenant_quotas — INI yang sekarang benar-benar dibaca
	// Append() untuk enforcement (bukan lagi projects.monthly_event_limit).
	_, err := pool.Exec(ctx, `
		INSERT INTO tenant_quotas (tenant_id, monthly_event_limit)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET monthly_event_limit = $2
	`, tenantID, monthlyLimit)
	if err != nil {
		t.Fatalf("failed to seed tenant quota: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM tenant_usage_counters WHERE tenant_id = $1`, tenantID)
		pool.Exec(context.Background(), `DELETE FROM tenant_quotas WHERE tenant_id = $1`, tenantID)
	})

	var projectID string
	err = pool.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, status, retention_days, monthly_event_limit)
		VALUES ($1, $2, 'active', 90, $3)
		RETURNING id
	`, tenantID, "events-test-"+time.Now().Format("20060102150405.000000000"), monthlyLimit).Scan(&projectID)
	if err != nil {
		t.Fatalf("failed to create events test project: %v", err)
	}
	t.Cleanup(func() {
		cleanupRetentionTestProject(t, context.Background(), pool, projectID)
	})

	return projectID
}

// ============================================================
// TEST: IngestHandler — success path
// ============================================================

func TestHandlers_IngestHandler_Success(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{
		ID:        "key-1",
		ProjectID: projectID,
		TenantID:  "events-test-tenant",
		Scope:     apikey.ScopeIngest,
	}
	app := newEventsTestApp(store, key)

	body := `{"event_type":"user.login","actor_id":"user-1","payload":{"ip":"127.0.0.1"}}`
	req := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST: IngestHandler — missing event_type rejected
// ============================================================

func TestHandlers_IngestHandler_MissingEventType_Rejected(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	body := `{"actor_id":"user-1","payload":{}}`
	req := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST: IngestHandler — quota exceeded
// ============================================================

func TestHandlers_IngestHandler_QuotaExceeded_Returns429(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// monthly_event_limit = 1: event pertama lolos, event kedua ditolak.
	// Pakai tenant UNIK sendiri (bukan "events-test-tenant" yang dipakai
	// test lain) supaya kuota per-tenant tidak bentrok dengan test lain
	// yang berjalan concurrent / dalam bulan yang sama.
	uniqueTenant := "events-test-tenant-quota-" + time.Now().Format("20060102150405.000000000")
	projectID := createTestProjectWithTenantQuota(t, ctx, pool, uniqueTenant, 1)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: uniqueTenant, Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	body := `{"event_type":"user.login","actor_id":"user-1","payload":{}}`

	// Event pertama: harus sukses.
	req1 := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
	req1.Header.Set("Authorization", "Bearer valid-key")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := app.Test(req1, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != 201 {
		t.Fatalf("expected first event to succeed with 201, got %d", resp1.StatusCode)
	}

	// Event kedua: harus ditolak karena kuota habis.
	req2 := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
	req2.Header.Set("Authorization", "Bearer valid-key")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := app.Test(req2, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 429 {
		t.Errorf("expected 429 quota_exceeded, got %d", resp2.StatusCode)
	}
}

// ============================================================
// TEST: IngestHandler — unauthenticated context (no API key)
// ============================================================

func TestHandlers_IngestHandler_Unauthenticated_Returns401(t *testing.T) {
	// store bisa nil di sini: request ditolak oleh RequireAPIKey
	// SEBELUM handler sempat menyentuh store sama sekali (tidak ada
	// Authorization header dikirim).
	store := NewPostgresStore(nil)
	app := newEventsTestApp(store, nil)

	body := `{"event_type":"user.login","payload":{}}`
	req := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
	// Sengaja TIDAK set Authorization header.

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ============================================================
// TEST: VerifyHandler — valid chain
// ============================================================

func TestHandlers_VerifyHandler_ValidChain(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	// Ingest 3 event lewat handler asli (bukan insert manual), supaya
	// hash chain terbentuk lewat jalur produksi yang sebenarnya.
	for i := 0; i < 3; i++ {
		body := `{"event_type":"chain.test","actor_id":"tester","payload":{"n":` + itoa(i) + `}}`
		req := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
		req.Header.Set("Authorization", "Bearer valid-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test failed on event %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 201 {
			t.Fatalf("expected event %d to ingest with 201, got %d", i, resp.StatusCode)
		}
	}

	// Verify seluruh chain.
	req := httptest.NewRequest("POST", "/v1/events/verify", jsonBody(`{}`))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if valid, _ := result["valid"].(bool); !valid {
		t.Errorf("expected valid=true for untampered chain, got %v", result)
	}
	if checked, _ := result["checked_count"].(float64); checked != 3 {
		t.Errorf("expected checked_count=3, got %v", result["checked_count"])
	}
}

// ============================================================
// TEST: VerifyHandler — detects tampering
// ============================================================

func TestHandlers_VerifyHandler_DetectsTampering(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	for i := 0; i < 2; i++ {
		body := `{"event_type":"chain.test","actor_id":"tester","payload":{"n":` + itoa(i) + `}}`
		req := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
		req.Header.Set("Authorization", "Bearer valid-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test failed on event %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 201 {
			t.Fatalf("expected event %d to ingest with 201, got %d", i, resp.StatusCode)
		}
	}

	// Tamper langsung di DB: ubah payload event sequence 1, TANPA
	// menghitung ulang hash — inilah yang harus terdeteksi VerifyHandler.
	_, err := pool.Exec(ctx, `
		UPDATE events SET payload = '{"n":"TAMPERED"}'
		WHERE project_id = $1 AND sequence = 1
	`, projectID)
	if err != nil {
		t.Fatalf("failed to tamper event: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/events/verify", jsonBody(`{}`))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if valid, _ := result["valid"].(bool); valid {
		t.Error("expected valid=false for tampered chain, got true")
	}
	if result["broken_at_event_id"] == nil || result["broken_at_event_id"] == "" {
		t.Error("expected broken_at_event_id to be set")
	}
}

// ============================================================
// TEST: QueryHandler — basic query returns ingested events
// ============================================================

func TestHandlers_QueryHandler_ReturnsIngestedEvents(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	body := `{"event_type":"query.test","actor_id":"tester","payload":{}}`
	req := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")
	ingestResp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	ingestResp.Body.Close()
	if ingestResp.StatusCode != 201 {
		t.Fatalf("expected ingest to succeed with 201, got %d", ingestResp.StatusCode)
	}

	queryReq := httptest.NewRequest("GET", "/v1/events?event_type=query.test", nil)
	queryReq.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(queryReq, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	events, _ := result["events"].([]any)
	if len(events) != 1 {
		t.Errorf("expected 1 event in query result, got %d", len(events))
	}
}

// ============================================================
// helpers
// ============================================================

// jsonBody membungkus string JSON sebagai io.Reader untuk body
// httptest.NewRequest. Pakai strings.NewReader (BUKAN reader custom)
// supaya httptest.NewRequest bisa mendeteksi tipenya lewat type-switch
// internal dan otomatis mengisi Content-Length — reader custom yang
// tidak dikenali akan membuat ContentLength tetap 0 dan body tidak
// terbaca dengan benar oleh BodyParser.
func jsonBody(s string) *strings.Reader {
	return strings.NewReader(s)
}

func itoa(n int) string {
	return formatInt64(int64(n))
}
