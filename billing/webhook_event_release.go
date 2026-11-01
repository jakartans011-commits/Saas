package billing

import (
	"context"
	"errors"
	"fmt"

	"trailseal/alerts"
)

// Release membatalkan klaim sebuah webhook event.
//
// Dipakai HANYA ketika handler gagal dengan error sementara dan kita
// membalas 5xx ke Stripe. Tanpa ini, baris klaim tertinggal di
// stripe_webhook_events, sehingga retry Stripe berikutnya dianggap
// duplikat dan dibalas 200 — event hilang permanen meskipun tidak
// pernah berhasil diproses.
//
// Aman dipanggil untuk event yang barisnya sudah tidak ada: DELETE yang
// tidak mengenai baris apa pun bukan error.
func (s *WebhookEventStore) Release(ctx context.Context, eventID string) error {
	if s == nil || s.pool == nil {
		return errors.New("webhook event store is not initialized")
	}
	if eventID == "" {
		return ErrWebhookEventIDRequired
	}
	if _, err := s.pool.Exec(
		ctx,
		`DELETE FROM stripe_webhook_events WHERE event_id = $1`,
		eventID,
	); err != nil {
		return fmt.Errorf("failed to release webhook event: %w", err)
	}
	return nil
}

// releaseWebhookClaim dipanggil WebhookHandler saat pemrosesan gagal.
//
// Kegagalan melepas klaim adalah kondisi yang tidak boleh diam: event itu
// sudah pasti tidak akan diproses ulang oleh retry Stripe, jadi seseorang
// harus memutarnya kembali secara manual dari dashboard Stripe.
func (s *Service) releaseWebhookClaim(eventID, eventType string) {
	if s.webhookEventStore == nil || eventID == "" {
		return
	}
	if err := s.webhookEventStore.Release(context.Background(), eventID); err != nil {
		alerts.NotifyCritical(
			"webhook_release_failed:"+eventID,
			"Stripe webhook event tersangkut",
			"Handler gagal dan klaim tidak bisa dilepas. Event ini tidak akan diproses ulang oleh retry Stripe; putar ulang manual dari dashboard Stripe.",
			map[string]string{
				"event_id":   eventID,
				"event_type": eventType,
			},
		)
	}
}