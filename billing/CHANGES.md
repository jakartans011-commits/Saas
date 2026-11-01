# Billing: Cancellation test + Downgrade test (item #1 & #2, Tingkat 1)

## ⚠️ Belum dijalankan — sandbox ini tidak punya Go toolchain

Tidak ada binary `go`, dan tidak ada akses network ke Go module proxy
(fiber, stripe-go, pgx tidak bisa di-fetch di sini). Semua kode di
bawah sudah saya review manual baris-per-baris, tapi **wajib Anda
jalankan sendiri**:

```bash
cd billing
go test ./... -v
```

Kalau ada error compile/assertion, kirim output-nya ke saya, saya
perbaiki cepat.

## File yang berubah

### `billing/service.go` (dimodifikasi, bukan fitur baru)

Dua dependency konkret diubah jadi interface kecil, supaya business
logic cancellation/downgrade bisa dites tanpa Postgres asli:

- `projectStore *project.PostgresStore` → `projectStore ProjectPlanApplier`
  (interface baru, 1 method: `ApplyPlanToTenant`)
- `webhookEventStore *WebhookEventStore` → `webhookEventStore WebhookEventClaimer`
  (interface baru, 1 method: `Claim`)

Pola ini sama seperti `tenant.TenantStore` yang sudah ada di repo Anda.

**Backward compatible** — `*project.PostgresStore` dan
`*billing.WebhookEventStore` tetap punya method yang sama persis,
jadi otomatis memenuhi interface baru. **Tidak ada perubahan di
`app.go`/`main.go`** — call site `billing.NewService(...)` yang sudah
ada tetap compile apa adanya. Import `"trailseal/project"` yang
sebelumnya dipakai di `service.go` sudah tidak diperlukan lagi (sudah
dihapus).

### `billing/service_test_helpers_test.go` (baru)

- `fakeProjectStore` — mencatat setiap panggilan `ApplyPlanToTenant`
  (tenant ID, limit, retention) supaya bisa diassert, bisa juga
  disuruh gagal untuk test failure path.
- `fakeWebhookEventStore` — versi in-memory dari idempotency check
  Postgres (`INSERT ... ON CONFLICT DO NOTHING`): Claim pertama untuk
  event ID sukses, Claim kedua untuk event ID yang sama gagal (sudah
  diklaim).
- `signStripePayload` — generate header `Stripe-Signature` yang valid
  (HMAC-SHA256 sesuai skema resmi Stripe), supaya test bisa lewat
  `WebhookHandler()` asli lengkap dengan verifikasi signature, bukan
  cuma testing logic-nya saja.

### `billing/cancellation_test.go` (baru) — item #1

Event `customer.subscription.deleted`:
- Tenant didowngrade ke Free, `SubscriptionStatus` jadi `"canceled"`
- Entitlement Free (5.000 events/bulan, retensi 14 hari) diterapkan ke
  semua project tenant
- Metadata `tenant_id` hilang → error
- Tenant tidak ditemukan → error
- **Kasus edge yang didokumentasikan (bukan bug baru, existing gap):**
  kalau `ApplyPlanToTenant` gagal setelah tenant sudah ditulis Free,
  tidak ada rollback — tenant record dan data project bisa
  inconsistent sementara. Test ini mendokumentasikan perilaku
  tersebut secara eksplisit.
- Level HTTP penuh lewat `WebhookHandler()`: signature valid → 200 +
  downgrade beneran terjadi; signature palsu → 400 + tenant TIDAK
  berubah; delivery duplikat (retry Stripe) → idempotent, logic
  downgrade cuma jalan sekali.

### `billing/downgrade_test.go` (baru) — item #2

Event `customer.subscription.updated`:
- Business → Pro (downgrade): tier & entitlement Pro diterapkan
- Pro → Business (upgrade): sengaja disertakan untuk membuktikan
  handler yang sama simetris — Stripe tidak mengirim event
  "downgrade" terpisah, semuanya lewat `subscription.updated`, resolve
  by price ID.
- Price ID tidak dikenal → gagal **fail-closed**: tenant plan TIDAK
  berubah, tidak ada propagasi ke project.
- Tidak ada line item → error.
- Metadata `tenant_id` hilang → error.
- Level HTTP: downgrade lewat webhook signature asli → 200 + tier
  berubah; price ID tak dikenal → 500 + tenant tidak berubah.

## Yang TIDAK saya sentuh

- `billing/plans.go`, `billing/webhook_event_store.go` — tidak ada
  perubahan.
- Tidak menambah endpoint/fitur baru — murni menambah test + membuka
  seam untuk testability.
