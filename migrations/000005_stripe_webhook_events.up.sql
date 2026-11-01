CREATE TABLE stripe_webhook_events (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT stripe_webhook_events_event_id_unique
        UNIQUE (event_id)
);

CREATE INDEX idx_stripe_webhook_events_created_at
    ON stripe_webhook_events (created_at DESC);