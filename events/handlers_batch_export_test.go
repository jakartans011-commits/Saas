package events

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trailseal/apikey"
)

// ============================================================
// BATCH INGEST HANDLER TESTS
//
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go test ./events -run TestHandlers_BatchIngestHandler -v
// ============================================================

func TestHandlers_BatchIngestHandler_AllSuccess(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	body := `{"events":[
		{"event_type":"batch.test","actor_id":"tester","payload":{"n":1}},
		{"event_type":"batch.test","actor_id":"tester","payload":{"n":2}},
		{"event_type":"batch.test","actor_id":"tester","payload":{"n":3}}
	]}`
	req := httptest.NewRequest("POST", "/v1/events/batch", jsonBody(body))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if sc, _ := result["success_count"].(float64); sc != 3 {
		t.Errorf("expected success_count=3, got %v", result["success_count"])
	}
	if tc, _ := result["total_count"].(float64); tc != 3 {
		t.Errorf("expected total_count=3, got %v", result["total_count"])
	}
}

func TestHandlers_BatchIngestHandler_EmptyEvents_Rejected(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	req := httptest.NewRequest("POST", "/v1/events/batch", jsonBody(`{"events":[]}`))
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

// TestHandlers_BatchIngestHandler_TooLarge_Rejected memastikan batch
// di atas maxBatchSize (500) ditolak SEBELUM satu pun event di-insert
// — cek ini murni validasi panjang array, tidak menyentuh DB, jadi
// aman dijalankan cepat meski 501 item.
func TestHandlers_BatchIngestHandler_TooLarge_Rejected(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	var sb strings.Builder
	sb.WriteString(`{"events":[`)
	for i := 0; i < maxBatchSize+1; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"event_type":"batch.test","actor_id":"tester","payload":{}}`)
	}
	sb.WriteString(`]}`)

	req := httptest.NewRequest("POST", "/v1/events/batch", jsonBody(sb.String()))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 batch_too_large, got %d", resp.StatusCode)
	}

	// Pastikan tidak ada satu pun event yang ke-insert walau batch ditolak.
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE project_id = $1`, projectID).Scan(&count); err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 events inserted for rejected oversized batch, got %d", count)
	}
}

// TestHandlers_BatchIngestHandler_PartialFailure_QuotaExceeded menguji
// SEMANTIK inti batch: kuota habis di TENGAH batch tidak membatalkan
// event yang sudah berhasil sebelumnya (bukan all-or-nothing) — lihat
// komentar besar di atas BatchIngestHandler di handlers.go.
func TestHandlers_BatchIngestHandler_PartialFailure_QuotaExceeded(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Kuota 2: dari 3 event dalam satu batch, hanya 2 pertama yang lolos.
	uniqueTenant := "events-test-tenant-batch-partial-" + time.Now().Format("20060102150405.000000000")
	projectID := createTestProjectWithTenantQuota(t, ctx, pool, uniqueTenant, 2)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: uniqueTenant, Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	body := `{"events":[
		{"event_type":"batch.test","actor_id":"tester","payload":{"n":1}},
		{"event_type":"batch.test","actor_id":"tester","payload":{"n":2}},
		{"event_type":"batch.test","actor_id":"tester","payload":{"n":3}}
	]}`
	req := httptest.NewRequest("POST", "/v1/events/batch", jsonBody(body))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	// 207 Multi-Status: sebagian sukses, sebagian gagal.
	if resp.StatusCode != 207 {
		t.Fatalf("expected 207 multi-status, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if sc, _ := result["success_count"].(float64); sc != 2 {
		t.Errorf("expected success_count=2, got %v", result["success_count"])
	}
	if tc, _ := result["total_count"].(float64); tc != 3 {
		t.Errorf("expected total_count=3, got %v", result["total_count"])
	}

	results, _ := result["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("expected 3 results entries, got %d", len(results))
	}
	last, _ := results[2].(map[string]any)
	if errCode, _ := last["error"].(string); errCode != "quota_exceeded" {
		t.Errorf("expected third result error=quota_exceeded, got %v", last["error"])
	}

	// Pastikan 2 event pertama BENAR-BENAR tersimpan di DB (bukan
	// cuma diklaim sukses di response), sesuai semantik partial-write.
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE project_id = $1`, projectID).Scan(&count); err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 events persisted despite partial batch failure, got %d", count)
	}
}

func TestHandlers_BatchIngestHandler_AllFail_QuotaExceededFromStart(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Kuota 0: setiap event dalam batch langsung ditolak.
	uniqueTenant := "events-test-tenant-batch-allfail-" + time.Now().Format("20060102150405.000000000")
	projectID := createTestProjectWithTenantQuota(t, ctx, pool, uniqueTenant, 0)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: uniqueTenant, Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	body := `{"events":[{"event_type":"batch.test","actor_id":"tester","payload":{}}]}`
	req := httptest.NewRequest("POST", "/v1/events/batch", jsonBody(body))
	req.Header.Set("Authorization", "Bearer valid-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 when every item in batch fails, got %d", resp.StatusCode)
	}
}

// ============================================================
// EXPORT HANDLER TESTS
//
//   go test ./events -run TestHandlers_ExportHandler -v
// ============================================================

func TestHandlers_ExportHandler_JSON(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	for i := 0; i < 3; i++ {
		body := `{"event_type":"export.test","actor_id":"tester","payload":{"n":` + itoa(i) + `}}`
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

	req := httptest.NewRequest("GET", "/v1/events/export?format=json", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if disp := resp.Header.Get("Content-Disposition"); !strings.Contains(disp, "attachment") {
		t.Errorf("expected Content-Disposition attachment header, got %q", disp)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if pid, _ := result["project_id"].(string); pid != projectID {
		t.Errorf("expected project_id=%s, got %v", projectID, result["project_id"])
	}
	if te, _ := result["total_events"].(float64); te != 3 {
		t.Errorf("expected total_events=3, got %v", result["total_events"])
	}
	events, _ := result["events"].([]any)
	if len(events) != 3 {
		t.Errorf("expected 3 events in export, got %d", len(events))
	}
}

func TestHandlers_ExportHandler_CSV(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	for i := 0; i < 2; i++ {
		body := `{"event_type":"export.csv.test","actor_id":"tester","payload":{"n":` + itoa(i) + `}}`
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

	req := httptest.NewRequest("GET", "/v1/events/export?format=csv&event_type=export.csv.test", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("expected Content-Type text/csv, got %q", ct)
	}

	r := csv.NewReader(resp.Body)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV response: %v", err)
	}
	// Header + 2 data rows.
	if len(rows) != 3 {
		t.Fatalf("expected 3 CSV rows (1 header + 2 data), got %d", len(rows))
	}
	if rows[0][0] != "id" || rows[0][4] != "event_type" {
		t.Errorf("unexpected CSV header: %v", rows[0])
	}
	for _, row := range rows[1:] {
		if row[4] != "export.csv.test" {
			t.Errorf("expected event_type=export.csv.test in CSV row, got %v", row)
		}
	}
}

func TestHandlers_ExportHandler_InvalidFormat_Rejected(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	req := httptest.NewRequest("GET", "/v1/events/export?format=xml", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 invalid_format, got %d", resp.StatusCode)
	}
}

func TestHandlers_ExportHandler_InvalidSinceParam_Rejected(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	// "since" bukan RFC3339 valid — harus ditolak lewat parseRFC3339,
	// sekaligus menutup coverage parseRFC3339 dan path error_since.
	req := httptest.NewRequest("GET", "/v1/events/export?since=not-a-date", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 invalid_since_format, got %d", resp.StatusCode)
	}
}

func TestHandlers_ExportHandler_MaxCapLimitsResults(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)

	store := NewPostgresStore(pool)
	key := &apikey.APIKey{ID: "key-1", ProjectID: projectID, TenantID: "events-test-tenant", Scope: apikey.ScopeIngest}
	app := newEventsTestApp(store, key)

	for i := 0; i < 5; i++ {
		body := `{"event_type":"export.cap.test","actor_id":"tester","payload":{"n":` + itoa(i) + `}}`
		req := httptest.NewRequest("POST", "/v1/events", jsonBody(body))
		req.Header.Set("Authorization", "Bearer valid-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test failed on event %d: %v", i, err)
		}
		resp.Body.Close()
	}

	// max=2 harus memotong hasil jadi 2 walau ada 5 event.
	req := httptest.NewRequest("GET", "/v1/events/export?format=json&event_type=export.cap.test&max=2", nil)
	req.Header.Set("Authorization", "Bearer valid-key")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if te, _ := result["total_events"].(float64); te != 2 {
		t.Errorf("expected total_events capped at 2, got %v", result["total_events"])
	}
}
