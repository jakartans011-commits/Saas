# Deployment Guide

## Prerequisites

- Go 1.25+
- PostgreSQL 16 (events and audit log)
- BoltDB, embedded: a local file used for tenants, users, projects and the email queue. It must live on persistent storage, and only one process can open it at a time.

## Build

```bash
go build -trimpath -o saas-api ./cmd/saas-api
```

The landing pages and portal templates are embedded in the binary, so no separate `web/` folder needs to ship next to it. The repository `Dockerfile` builds the same binary.

## Production configuration

Set `APP_ENV=production` to make the server refuse to start when required configuration is missing or not served over HTTPS. Only the variable names are reported, never their values.

Required in production:

| Variable                                    | Purpose                                                              |
| ------------------------------------------- | -------------------------------------------------------------------- |
| `APP_ENV=production`                        | Enables the startup checks below                                     |
| `COOKIE_SECURE=true`                        | Sets the `Secure` flag on auth cookies (requires HTTPS)              |
| `CORS_ALLOWED_ORIGINS`                      | Comma-separated allowed origins, all `https://`                      |
| `APP_BASE_URL`                              | Public base URL used in email links, `https://`                      |
| `FRONTEND_BASE_URL`                         | If set, must be `https://`                                           |
| `STRIPE_SECRET_KEY`                         | Stripe API key                                                       |
| `STRIPE_WEBHOOK_SECRET`                     | Webhook signing secret. When empty, the webhook endpoint answers 503 |
| `STRIPE_PRICE_PRO`, `STRIPE_PRICE_BUSINESS` | Monthly Stripe price IDs                                             |
| `BILLING_SUCCESS_URL`, `BILLING_CANCEL_URL` | Checkout return URLs, `https://`                                     |
| `RESEND_API_KEY`                            | Email delivery (see Email)                                           |
| `NOTIFY_FROM_EMAIL`                         | Sender address; its domain must be verified in Resend                |
| `ALERT_WEBHOOK_URL`                         | Slack or Discord webhook for critical alerts                         |

Optional: `ALERT_WEBHOOK_PROVIDER` (`slack` default, or `discord`), `SENTRY_DSN`, `STRIPE_PRICE_PRO_ANNUAL`, `STRIPE_PRICE_BUSINESS_ANNUAL` (leave empty to keep annual billing disabled), `STRIPE_PRICE_BUSINESS_LEGACY` (only if customers remain on the old Business price).

Keep secrets out of git. `.env.ps1` and `data/` are git-ignored; commit only `.env.example`, with names but no values.

## Background jobs

`saas-api` runs a daily scheduler in-process (first run 2 minutes after start, then every 24 hours). Each run:

1. Purges events older than each project's retention window (14 / 180 / 365 days by plan).
2. Applies audit-log retention per active tenant, clamped to the plan ceiling.
3. Permanently deletes tenants that were soft-deleted and whose recovery window has passed (30 days Free/Pro, 60 days Business).

It also downgrades tenants whose payment grace period has expired.

Run the scheduler on a single `saas-api` instance. The jobs are idempotent, but running several instances repeats the same work.

The standalone `retention-job` binary is no longer required for retention when `saas-api` is running.

### Email

Transactional email (verification, password reset) is sent through Resend by `saas-api` when `RESEND_API_KEY` is set. The sender is `NOTIFY_FROM_EMAIL`, and its domain must be verified in Resend. Without a key, these emails are only written to the log, and new email/password accounts cannot log in because verification is required. OAuth sign-in is not affected.

Quota and chain-integrity notifications are queued and drained by a scheduler inside `saas-api`, which also requires `RESEND_API_KEY`. A job that fails 5 times is parked in the queue and logged, so it no longer blocks other notifications.

Do not run `notify-worker` alongside `saas-api`. BoltDB can be opened by only one process, and `notify-worker` falls back to a logging sender that discards queued jobs when no key is set.
