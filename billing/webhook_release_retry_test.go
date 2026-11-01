package billing

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/tenant"
)

// newSubscriptionUpdatedRetryPayload membangun envelope Stripe
// "customer.subscription.updated" yang valid untuk test ini.
func newSubscriptionUpdatedRetryPayload(eventID, tenantID, priceID string) []byte {
	env := map[string]interface{}{
		"id":   eventID,
		"type": "customer.subscription.updated",
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"id":       "sub_retry_1",
				"status":   "active",
				"metadata": map[string]string{"tenant_id": tenantID},
				"items": map[string]interface{}{
					"data": []interface{}{
						map[string]interface{}{
							"price": map[string]interface{}{"id": priceID},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(env)
	return body
}

func postStripeWebhook(t *testing.T, app *fiber.App, secret string, payload []byte) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", signStripePayload(secret, payload, time.Now()))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp.StatusCode
}

// TestWebhookHandler_ReleasesClaimOnFailure_RetryIsProcessed mengunci
// perbaikan kehilangan event.
//
// Sebelum perbaikan: Claim menyisipkan baris sebelum pemrosesan dan tidak
// pernah menghapusnya. Handler yang gagal membalas 500 supaya Stripe retry,
// tetapi retry itu dianggap duplikat dan dibalas 200 — pelanggan sudah
// dibayar sementara plannya tidak pernah naik, tanpa jejak kegagalan.
//
// Test ini memastikan percobaan kedua benar-benar menjalankan ulang logika
// bisnis, bukan dilewati sebagai duplikat.
func TestWebhookHandler_ReleasesClaimOnFailure_RetryIsProcessed(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                 "acme-corp",
		PlanTier:           "free",
		SubscriptionStatus: "active",
	})

	projectStore := &fakeProjectStore{err: errors.New("simulated project store outage")}
	webhookStore := newFakeWebhookEventStore()
	svc := newTestService(t, tenantStore, projectStore, webhookStore)

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionUpdatedRetryPayload("evt_retry_1", "acme-corp", "price_pro_test")

	// Pengiriman pertama: project store sedang gagal.
	if code := postStripeWebhook(t, app, svc.webhookSecret, payload); code != http.StatusInternalServerError {
		t.Fatalf("pengiriman pertama: expected 500, got %d", code)
	}
	if n := webhookStore.releasedCount(); n != 1 {
		t.Fatalf("klaim tidak dilepas setelah kegagalan: releasedCount = %d, want 1", n)
	}
	if n := projectStore.callCount(); n != 0 {
		t.Fatalf("pengiriman pertama seharusnya tidak mencatat apply: callCount = %d, want 0", n)
	}

	// Retry Stripe atas event_id yang SAMA, kali ini project store pulih.
	projectStore.err = nil
	if code := postStripeWebhook(t, app, svc.webhookSecret, payload); code != http.StatusOK {
		t.Fatalf("retry: expected 200, got %d", code)
	}

	// Inti pembuktian: retry benar-benar memproses ulang. Kalau klaim tidak
	// dilepas, handler membalas 200 "duplicate" dan callCount tetap 0.
	if n := projectStore.callCount(); n != 1 {
		t.Fatalf("retry tidak diproses ulang: callCount = %d, want 1", n)
	}

	got, err := tenantStore.Get("acme-corp")
	if err != nil {
		t.Fatalf("failed to reload tenant: %v", err)
	}
	if got.PlanTier != string(PlanPro) {
		t.Errorf("expected plan tier %q, got %q", PlanPro, got.PlanTier)
	}
}

// TestWebhookHandler_DuplicateDeliveryStillSkipped memastikan perbaikan di
// atas tidak melemahkan idempotency: pengiriman ganda yang BERHASIL tetap
// hanya diproses sekali.
func TestWebhookHandler_DuplicateDeliveryStillSkipped(t *testing.T) {
	tenantStore := tenant.NewInMemoryTenantStore()
	seedTenant(t, tenantStore, &tenant.Tenant{
		ID:                 "acme-corp",
		PlanTier:           "free",
		SubscriptionStatus: "active",
	})

	projectStore := &fakeProjectStore{}
	webhookStore := newFakeWebhookEventStore()
	svc := newTestService(t, tenantStore, projectStore, webhookStore)

	app := fiber.New()
	app.Post("/api/billing/webhook", svc.WebhookHandler())

	payload := newSubscriptionUpdatedRetryPayload("evt_dup_1", "acme-corp", "price_pro_test")

	for i := 1; i <= 2; i++ {
		if code := postStripeWebhook(t, app, svc.webhookSecret, payload); code != http.StatusOK {
			t.Fatalf("pengiriman ke-%d: expected 200, got %d", i, code)
		}
	}

	if n := projectStore.callCount(); n != 1 {
		t.Errorf("pengiriman ganda diproses %d kali, want 1", n)
	}
	if n := webhookStore.releasedCount(); n != 0 {
		t.Errorf("tidak boleh ada pelepasan klaim pada jalur sukses: releasedCount = %d", n)
	}
}
