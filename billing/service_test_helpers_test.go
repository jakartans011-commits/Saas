package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// ============================================================
// fakeProjectStore — in-memory stand-in for ProjectPlanApplier.
//
// Records every call so tests can assert not just the tenant's
// final state, but that the entitlement propagation to projects
// actually happened with the right numbers.
// ============================================================

type applyPlanCall struct {
	tenantID          string
	monthlyEventLimit int64
	retentionDays     int
}

type fakeProjectStore struct {
	mu    sync.Mutex
	calls []applyPlanCall
	// err, when set, is returned by every ApplyPlanToTenant call
	// instead of recording it — used to test failure propagation.
	err error
}

func (f *fakeProjectStore) ApplyPlanToTenant(_ context.Context, tenantID string, monthlyEventLimit int64, retentionDays int) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, applyPlanCall{
		tenantID:          tenantID,
		monthlyEventLimit: monthlyEventLimit,
		retentionDays:     retentionDays,
	})
	return nil
}

func (f *fakeProjectStore) lastCall() (applyPlanCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return applyPlanCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

func (f *fakeProjectStore) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// ============================================================
// fakeWebhookEventStore — in-memory stand-in for WebhookEventClaimer.
//
// Mirrors the real store's contract: first Claim() for a given
// eventID succeeds (true), every subsequent Claim() for the same
// eventID reports it as already claimed (false) — same semantics
// as the Postgres INSERT ... ON CONFLICT DO NOTHING.
// ============================================================

type fakeWebhookEventStore struct {
	mu     sync.Mutex
	events map[string]string // eventID -> eventType
	// claimErr, when set, is returned by every Claim call.
	claimErr error
	// releaseErr, when set, is returned by every Release call.
	releaseErr error
	// releasedIDs records every event ID whose claim was released,
	// in order, so tests can prove the handler released on failure.
	releasedIDs []string
}

func newFakeWebhookEventStore() *fakeWebhookEventStore {
	return &fakeWebhookEventStore{events: make(map[string]string)}
}

func (f *fakeWebhookEventStore) Claim(_ context.Context, eventID string, eventType string) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.events[eventID]; exists {
		return false, nil
	}
		f.events[eventID] = eventType
	return true, nil
}

// Release mencerminkan WebhookEventStore.Release: menghapus klaim supaya
// retry Stripe dapat memproses ulang event yang sama.
func (f *fakeWebhookEventStore) Release(_ context.Context, eventID string) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.events, eventID)
	f.releasedIDs = append(f.releasedIDs, eventID)
	return nil
}

// releasedCount adalah jumlah pelepasan klaim yang tercatat.
func (f *fakeWebhookEventStore) releasedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.releasedIDs)
}

// ============================================================
// signStripePayload — builds a valid Stripe-Signature header value
// for a raw webhook payload, per Stripe's documented HMAC scheme:
//
//	signed_payload = "<timestamp>.<raw body>"
//	signature      = hex(HMAC_SHA256(webhookSecret, signed_payload))
//	header         = "t=<timestamp>,v1=<signature>"
//
// This lets tests exercise Service.WebhookHandler() end-to-end
// (including real signature verification via stripe-go's
// webhook.ConstructEventWithOptions) without contacting Stripe.
// ============================================================

func signStripePayload(secret string, payload []byte, ts time.Time) string {
	signedPayload := fmt.Sprintf("%d.%s", ts.Unix(), payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), sig)
}
