package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/apikey"
	"trailseal/middleware"
)

// ============================================================
// P2-2 jalur A: export events dengan cap plan (Free = 100 terbaru)
//
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go test ./events -run ExportCap -v
//
// Satu seed 150 event dipakai ulang oleh semua subtest supaya
// kuota tenant test tidak terpakai berulang.
// ============================================================

func newExportCapTestApp(store *PostgresStore, key *apikey.APIKey, exportCap int) *fiber.App {
	app := fiber.New()
	app.Use(middleware.RequireAPIKey(&eventsMockAPIKeyStore{key: key}))
	app.Get("/v1/events/export", ExportHandler(store, func(string) int { return exportCap }))
	return app
}

// exportSequences membaca body export JSON (array atau objek
// dengan field "events") dan mengembalikan urutan sequence.
func exportSequences(t *testing.T, body io.Reader) []int64 {
	t.Helper()

	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read export body: %v", err)
	}

	type item struct {
		Sequence int64 `json:"sequence"`
	}

	var items []item
	if err := json.Unmarshal(raw, &items); err != nil {
		var env struct {
			Events []item `json:"events"`
		}
		if err2 := json.Unmarshal(raw, &env); err2 != nil {
			t.Fatalf("export body is neither array nor {events:[...]}: %v / %v", err, err2)
		}
		items = env.Events
	}

	out := make([]int64, len(items))
	for i, it := range items {
		out[i] = it.Sequence
	}
	return out
}

func assertAscendingRange(t *testing.T, got []int64, first, last int64) {
	t.Helper()

	want := int(last - first + 1)
	if len(got) != want {
		t.Fatalf("expected %d events (%d..%d), got %d", want, first, last, len(got))
	}
	for i, seq := range got {
		if seq != first+int64(i) {
			t.Fatalf("index %d: expected sequence %d, got %d (must be ascending %d..%d)",
				i, first+int64(i), seq, first, last)
		}
	}
}

func TestExportCap_LatestEventsForFreePlan(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	projectID := createTestProjectWithLimit(t, ctx, pool, 100000)
	store := NewPostgresStore(pool)

	const total = 150
	for i := 1; i <= total; i++ {
		req := &IngestRequest{
			EventType: "export.cap.test",
			ActorID:   "tester",
			Payload:   json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)),
		}
		if _, err := store.Append(ctx, projectID, "events-test-tenant", req, fmt.Sprintf("export-cap-%d", i)); err != nil {
			t.Fatalf("seed event %d failed: %v", i, err)
		}
	}

	key := &apikey.APIKey{
		ID:        "key-export-cap",
		ProjectID: projectID,
		TenantID:  "events-test-tenant",
		Scope:     apikey.ScopeIngest,
	}

	doExport := func(t *testing.T, exportCap int, query string) (*fiber.App, []int64, map[string]string) {
		t.Helper()
		app := newExportCapTestApp(store, key, exportCap)
		req := httptest.NewRequest("GET", "/v1/events/export"+query, nil)
		req.Header.Set("Authorization", "Bearer valid-key")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		hdr := map[string]string{
			"capped":  resp.Header.Get("X-Export-Capped"),
			"planCap": resp.Header.Get("X-Export-Plan-Cap"),
		}
		return app, exportSequences(t, resp.Body), hdr
	}

	t.Run("QueryLatest returns newest 100 ascending", func(t *testing.T) {
		got, err := store.QueryLatest(ctx, projectID, QueryFilter{}, 100)
		if err != nil {
			t.Fatalf("QueryLatest failed: %v", err)
		}
		seqs := make([]int64, len(got))
		for i, e := range got {
			seqs[i] = e.Sequence
		}
		assertAscendingRange(t, seqs, 51, 150)
	})

	t.Run("cap 100 gives newest 100 plus headers", func(t *testing.T) {
		_, seqs, hdr := doExport(t, 100, "")
		assertAscendingRange(t, seqs, 51, 150)
		if hdr["capped"] != "true" {
			t.Errorf("expected X-Export-Capped=true, got %q", hdr["capped"])
		}
		if hdr["planCap"] != "100" {
			t.Errorf("expected X-Export-Plan-Cap=100, got %q", hdr["planCap"])
		}
	})

	t.Run("cap 0 gives everything without headers", func(t *testing.T) {
		_, seqs, hdr := doExport(t, 0, "")
		assertAscendingRange(t, seqs, 1, 150)
		if hdr["capped"] != "" {
			t.Errorf("expected no X-Export-Capped header, got %q", hdr["capped"])
		}
		if hdr["planCap"] != "" {
			t.Errorf("expected no X-Export-Plan-Cap header, got %q", hdr["planCap"])
		}
	})

	t.Run("max below cap wins and still newest", func(t *testing.T) {
		_, seqs, _ := doExport(t, 100, "?max=20")
		assertAscendingRange(t, seqs, 131, 150)
	})
}
