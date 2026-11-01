package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/tenant"
)

// ============================================================
// WebhookEventStore (Postgres) â€” previously 0% coverage because
// every WebhookHandler test in cancellation_test.go / downgrade_test.go
// uses fakeWebhookEventStore. This exercises the REAL implementation,
// whose idempotency guarantee rests on the DB's
// UNIQUE(event_id) + ON CONFLICT DO NOTHING â€” that guarantee cannot
// be verified by an in-memory fake.
//
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go test ./billing -run TestWebhookEventStore -v
// ============================================================

func billingTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TRAILSEAL_TEST_POSTGRES_URL is not set; billing postgres integration tests skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping postgres: %v", err)
	}

	// FIX: close via t.Cleanup (LIFO), so this runs AFTER the
	// t.Cleanup(cleanupWebhookEvent(...)) registered by each test --
	// otherwise a `defer pool.Close()` right after this call closes
	// the pool BEFORE cleanup's DELETE ever runs, leaking rows.
	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func cleanupWebhookEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DELETE FROM stripe_webhook_events WHERE event_id = $1`, eventID); err != nil {
		t.Logf("cleanup webhook event %s: %v", eventID, err)
	}
}

func TestWebhookEventStore_Claim_FirstTimeSucceeds(t *testing.T) {
	pool := billingTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := NewWebhookEventStore(pool)
	eventID := "evt_store_test_" + time.Now().Format("20060102150405.000000000")
	t.Cleanup(func() { cleanupWebhookEvent(t, context.Background(), pool, eventID) })

	claimed, err := store.Claim(ctx, eventID, "checkout.session.completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !claimed {
		t.Error("expected first claim to succeed (claimed=true)")
	}
}

// TestWebhookEventStore_Claim_DuplicateReturnsFalse is the whole point
// of this store: this is the actual idempotency guarantee (backed by
// UNIQUE(event_id) + ON CONFLICT DO NOTHING) that fakeWebhookEventStore
// only simulates in-memory.
func TestWebhookEventStore_Claim_DuplicateReturnsFalse(t *testing.T) {
	pool := billingTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := NewWebhookEventStore(pool)
	eventID := "evt_store_dup_" + time.Now().Format("20060102150405.000000000")
	t.Cleanup(func() { cleanupWebhookEvent(t, context.Background(), pool, eventID) })

	first, err := store.Claim(ctx, eventID, "checkout.session.completed")
	if err != nil {
		t.Fatalf("unexpected error on first claim: %v", err)
	}
	if !first {
		t.Fatal("expected first claim to succeed")
	}

	second, err := store.Claim(ctx, eventID, "checkout.session.completed")
	if err != nil {
		t.Fatalf("unexpected error on duplicate claim: %v", err)
	}
	if second {
		t.Error("expected duplicate claim to return claimed=false")
	}
}

func TestWebhookEventStore_Claim_EmptyEventID_Rejected(t *testing.T) {
	pool := billingTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := NewWebhookEventStore(pool)

	_, err := store.Claim(ctx, "", "checkout.session.completed")
	if !errors.Is(err, ErrWebhookEventIDRequired) {
		t.Errorf("expected ErrWebhookEventIDRequired, got %v", err)
	}
}

func TestWebhookEventStore_Claim_EmptyEventType_Rejected(t *testing.T) {
	pool := billingTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := NewWebhookEventStore(pool)

	_, err := store.Claim(ctx, "evt_missing_type", "")
	if !errors.Is(err, ErrWebhookEventTypeRequired) {
		t.Errorf("expected ErrWebhookEventTypeRequired, got %v", err)
	}
}

func TestWebhookEventStore_Exists(t *testing.T) {
	pool := billingTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := NewWebhookEventStore(pool)
	eventID := "evt_store_exists_" + time.Now().Format("20060102150405.000000000")
	t.Cleanup(func() { cleanupWebhookEvent(t, context.Background(), pool, eventID) })

	existsBefore, err := store.Exists(ctx, eventID)
	if err != nil {
		t.Fatalf("unexpected error checking Exists before claim: %v", err)
	}
	if existsBefore {
		t.Error("expected Exists=false before the event is claimed")
	}

	if _, err := store.Claim(ctx, eventID, "customer.subscription.updated"); err != nil {
		t.Fatalf("failed to claim event: %v", err)
	}

	existsAfter, err := store.Exists(ctx, eventID)
	if err != nil {
		t.Fatalf("unexpected error checking Exists after claim: %v", err)
	}
	if !existsAfter {
		t.Error("expected Exists=true after the event is claimed")
	}
}

func TestWebhookEventStore_GetByEventID_Found(t *testing.T) {
	pool := billingTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := NewWebhookEventStore(pool)
	eventID := "evt_store_get_" + time.Now().Format("20060102150405.000000000")
	t.Cleanup(func() { cleanupWebhookEvent(t, context.Background(), pool, eventID) })

	if _, err := store.Claim(ctx, eventID, "customer.subscription.deleted"); err != nil {
		t.Fatalf("failed to claim event: %v", err)
	}

	eventType, _, _, err := store.GetByEventID(ctx, eventID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventType != "customer.subscription.deleted" {
		t.Errorf("expected event_type %q, got %q", "customer.subscription.deleted", eventType)
	}
}

func TestWebhookEventStore_GetByEventID_NotFound(t *testing.T) {
	pool := billingTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := NewWebhookEventStore(pool)

	_, _, _, err := store.GetByEventID(ctx, "evt_never_existed_12345")
	if err == nil {
		t.Fatal("expected error for an event ID that was never claimed")
	}
}

// TestWebhookEventStore_NilStore_FailsClosed does not need Postgres:
// s == nil is checked at the top of every method, on purpose, so a
// misconfigured (never-initialized) store fails closed instead of
// panicking on a nil pointer dereference or nil pool.
func TestWebhookEventStore_NilStore_FailsClosed(t *testing.T) {
	var store *WebhookEventStore

	if _, err := store.Claim(context.Background(), "evt_x", "some.type"); err == nil {
		t.Error("expected error from Claim on a nil store")
	}
	if _, err := store.Exists(context.Background(), "evt_x"); err == nil {
		t.Error("expected error from Exists on a nil store")
	}
	if _, _, _, err := store.GetByEventID(context.Background(), "evt_x"); err == nil {
		t.Error("expected error from GetByEventID on a nil store")
	}
}

// ============================================================
// WebhookHandler edge cases NOT covered by cancellation_test.go /
// downgrade_test.go / checkout_completed_test.go.
// ============================================================

// TestWebhookHandler_UnrecognizedEventType_IsIgnoredButStillClaimed
// covers the `default:` branch in WebhookHandler's event.Type switch.
// An event type TrailSeal doesn't act on must still return 200 and
// must still consume its idempotency slot (so a retry of the SAME
// unrecognized event is also a no-op, not reprocessed).
func TestWebhookHandler_UnrecognizedEventType_IsIgnoredButStillClaimed(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp", PlanTier: "pro"})
	webhookStore := newFakeWebhookEventStore()
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, webhookStore)

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	env := map[string]interface{}{
		"id":   "evt_unrecognized_1",
		"type": "customer.created", // not handled by any case
		"data": map[string]interface{}{"object": map[string]interface{}{}},
	}
	payload, _ := json.Marshal(env)
	sig := signStripePayload(svc.webhookSecret, payload, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", sig)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for an ignored event type, got %d", resp.StatusCode)
	}

	// Tenant must be completely untouched.
	got, _ := tenantStore.Get("acme-corp")
	if got.PlanTier != "pro" {
		t.Errorf("expected tenant untouched, got plan tier %q", got.PlanTier)
	}

	// The event ID must still have been claimed, so a retry of this
	// exact (unrecognized) event is recognized as a duplicate.
	if _, exists := webhookStore.events["evt_unrecognized_1"]; !exists {
		t.Error("expected unrecognized event to still be claimed for idempotency")
	}
}

// TestWebhookHandler_WebhookEventStoreNil_Returns500 covers the
// `s.webhookEventStore == nil` guard directly.
func TestWebhookHandler_WebhookEventStoreNil_Returns500(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp"})
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, nil)

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionDeletedPayload("evt_no_store", "acme-corp")
	sig := signStripePayload(svc.webhookSecret, payload, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", sig)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 when webhook event store is nil, got %d", resp.StatusCode)
	}
}

// TestWebhookHandler_ClaimError_Returns500 covers the error path when
// the claim itself fails (e.g. DB unreachable) â€” distinct from the
// "already claimed" (duplicate) case, which is a normal 200 response.
func TestWebhookHandler_ClaimError_Returns500(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{ID: "acme-corp"})
	webhookStore := newFakeWebhookEventStore()
	webhookStore.claimErr = errors.New("db unreachable")
	svc := newTestService(t, tenantStore, &fakeProjectStore{}, webhookStore)

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionDeletedPayload("evt_claim_err", "acme-corp")
	sig := signStripePayload(svc.webhookSecret, payload, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", sig)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 when Claim itself errors, got %d", resp.StatusCode)
	}
}