package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrWebhookEventIDRequired   = errors.New("webhook event id is required")
	ErrWebhookEventTypeRequired = errors.New("webhook event type is required")
)

// WebhookEventStore menyimpan Stripe webhook event yang sudah di-claim.
//
// Idempotency dijaga oleh UNIQUE(event_id) di PostgreSQL.
// Claim() menggunakan INSERT ... ON CONFLICT DO NOTHING sehingga
// dua webhook identik yang datang bersamaan tidak bisa sama-sama
// memperoleh status claimed=true.
type WebhookEventStore struct {
	pool *pgxpool.Pool
}

func NewWebhookEventStore(pool *pgxpool.Pool) *WebhookEventStore {
	return &WebhookEventStore{
		pool: pool,
	}
}

// Claim mencoba mengklaim sebuah Stripe webhook event.
//
// Return:
//   - true  -> event baru, caller boleh memproses event
//   - false -> event sudah pernah di-claim, caller harus skip
//
// Penting:
// jangan melakukan SELECT terlebih dahulu lalu INSERT.
// INSERT ... ON CONFLICT DO NOTHING adalah atomic terhadap
// UNIQUE(event_id), sehingga aman terhadap duplicate/concurrent delivery.
func (s *WebhookEventStore) Claim(
	ctx context.Context,
	eventID string,
	eventType string,
) (bool, error) {
	if s == nil || s.pool == nil {
		return false, errors.New("webhook event store is not initialized")
	}

	if eventID == "" {
		return false, ErrWebhookEventIDRequired
	}

	if eventType == "" {
		return false, ErrWebhookEventTypeRequired
	}

	tag, err := s.pool.Exec(
		ctx,
		`
		INSERT INTO stripe_webhook_events (
			event_id,
			event_type
		)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
		`,
		eventID,
		eventType,
	)
	if err != nil {
		return false, fmt.Errorf(
			"failed to claim webhook event: %w",
			err,
		)
	}

	return tag.RowsAffected() == 1, nil
}

// Exists digunakan untuk memeriksa apakah event sudah pernah
// tersimpan. Claim tetap menjadi primitive utama untuk idempotency.
func (s *WebhookEventStore) Exists(
	ctx context.Context,
	eventID string,
) (bool, error) {
	if s == nil || s.pool == nil {
		return false, errors.New("webhook event store is not initialized")
	}

	if eventID == "" {
		return false, ErrWebhookEventIDRequired
	}

	var exists bool

	err := s.pool.QueryRow(
		ctx,
		`
		SELECT EXISTS (
			SELECT 1
			FROM stripe_webhook_events
			WHERE event_id = $1
		)
		`,
		eventID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf(
			"failed to check webhook event: %w",
			err,
		)
	}

	return exists, nil
}

// GetByEventID mengambil record webhook event berdasarkan Stripe event ID.
//
// Method ini terutama berguna untuk test, observability, dan debugging.
// Tidak digunakan sebagai mekanisme utama idempotency.
func (s *WebhookEventStore) GetByEventID(
	ctx context.Context,
	eventID string,
) (eventType string, processedAt interface{}, createdAt interface{}, err error) {
	if s == nil || s.pool == nil {
		return "", nil, nil, errors.New(
			"webhook event store is not initialized",
		)
	}

	if eventID == "" {
		return "", nil, nil, ErrWebhookEventIDRequired
	}

	err = s.pool.QueryRow(
		ctx,
		`
		SELECT
			event_type,
			processed_at,
			created_at
		FROM stripe_webhook_events
		WHERE event_id = $1
		`,
		eventID,
	).Scan(
		&eventType,
		&processedAt,
		&createdAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil, fmt.Errorf(
			"webhook event not found: %s",
			eventID,
		)
	}

	if err != nil {
		return "", nil, nil, fmt.Errorf(
			"failed to get webhook event: %w",
			err,
		)
	}

	return eventType, processedAt, createdAt, nil
}
