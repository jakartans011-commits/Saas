# Audit: memindahkan bbolt ke Postgres

**BUTIR 7 — audit saja. Tidak ada kode yang diubah oleh dokumen ini.**
Disusun 2026-11-01.

---

## Kenapa ini harus terjadi

bbolt mengunci berkasnya untuk **satu proses**. Empat akibat, semuanya permanen selama arsitekturnya begini:

| Akibat | Bukti di repo ini |
|---|---|
| Hanya satu instance; tidak bisa autoscale | `bolt.Open` di `main.go:76` dengan `Timeout: 5s` |
| Setiap deploy = downtime | Proses lama harus melepas lock sebelum yang baru start |
| Crash meninggalkan lock | **Sudah terjadi** — `backend.db.lock` dan `dashboard.db.lock` tertinggal di `data/` |
| Data hanya ada di satu disk | Butuh endpoint backup khusus (BUTIR 3); Postgres tidak |

Plus satu yang tidak kelihatan sampai ramai: **plafon throughput per tenant.** Rate limiter menulis satu transaksi bbolt untuk setiap request (`rate_limit.go:506`), dan `r.mu.Lock()` membuat semua request satu tenant berbaris satu per satu.

**Bukan alasan menunda peluncuran.** Satu instance cukup untuk 100 pelanggan pertama. Kerjakan **sebelum pelanggan ke-20**, saat masih sepi.

---

## Apa yang masih di bbolt

Diurutkan dari yang paling mudah dipindahkan ke yang paling sulit.

### Kelompok A — ephemeral, boleh hilang saat migrasi

Tidak ada nilai historis. Yang terburuk terjadi: beberapa pengguna perlu login ulang, atau satu jendela rate limit direset.

| Data | Berkas | Jalur panas? | Catatan |
|---|---|---|---|
| Rate limit windows | `middleware/rate_limit.go` | **YA — setiap request** | Kandidat pertama. Lihat catatan di bawah. |
| Refresh token | `auth/refresh_token_store_bbolt.go` | Saat login/refresh | Rotasi + deteksi reuse harus tetap atomik |
| Access token revocation | `auth/access_token_revocation_store_bbolt.go` | Setiap request terautentikasi | Daftar pendek, TTL pendek |
| Token verifikasi email | `auth/email_verification_store_bbolt.go` | Jarang | Sepele |
| Token reset password | `auth/password_reset_store_bbolt.go` | Jarang | Sepele |
| Antrean email | `notify.NewBoltEmailQueue` | Per notifikasi | Hati-hati: antrean di dua tempat = email ganda |

### Kelompok B — data produk, TIDAK boleh hilang

Butuh migrasi dengan verifikasi jumlah baris sebelum dan sesudah.

| Data | Berkas | Jalur panas? |
|---|---|---|
| Tenant | `tenant/tenant_store_bbolt.go` | Setiap resolusi plan |
| User | `auth/user_store_bbolt.go` | Setiap login |
| Role + custom role | `rbac/role_store_bbolt.go` | Setiap pemeriksaan capability |
| Agent + tool | `agent/store_bbolt.go` | Setiap panggilan MCP |
| Custom prompt | `mcp/custom_prompt_store_bbolt.go` | `prompts/get` |
| Usage event | `usage/event_store_bbolt.go` | Setiap panggilan MCP |
| Audit event (jalur bbolt) | `audit/store_bbolt.go` | Setiap request yang diaudit |

### Catatan khusus: kuota ganda

Sudah tercatat sebagai risiko yang diterima, dan migrasi ini adalah saat yang tepat menutupnya. Ada **dua** penghitung event dengan semantik reset berbeda:

- `tenant.CurrentEventCount` di bbolt (jalur MCP, lewat `TryConsumeQuota`)
- `tenant_usage_counters` di Postgres (jalur ingest, dipakai `events/quota_usage.go`)

Dashboard sudah membaca yang Postgres sejak perbaikan 2026-11-01. Setelah `tenant` pindah ke Postgres, keduanya bisa jadi satu — dan `TryConsumeQuota` menjadi satu `UPDATE ... RETURNING`, yang juga menghapus plafon throughput di atas.

---

## Urutan migrasi

Setiap langkah berdiri sendiri, bisa di-deploy, dan bisa dibatalkan.

**1. Rate limit windows** (~0,5 hari)
Paling mudah dan paling bernilai: menghapus satu transaksi bbolt dari **setiap request**, sekaligus menghapus plafon throughput per tenant. Datanya ephemeral — kehilangan satu jendela berarti satu menit batas yang longgar, bukan kerugian. Antarmukanya sudah terisolasi di `rateCounter.Allow()`.

*Catatan:* satu `INSERT ... ON CONFLICT DO UPDATE ... RETURNING` per request ke Neon lewat jaringan bisa lebih lambat dari bbolt lokal. Ukur dulu. Alternatifnya penghitung di memori dengan periode flush — tapi itu berarti batas tidak dibagi antar instance, yang baru penting setelah instance kedua ada.

**2. Token (refresh, revocation, verifikasi email, reset password)** (~1 hari)
Satu tabel, kolom `expires_at`, indeks di `token_hash`. Deteksi reuse yang sudah ada (`RevokeAllSessionsIfTokenReused`, dengan `ReuseDetectionLeeway`) dipindahkan apa adanya; test yang ada menjaganya.

**3. Tenant + user + role** (~1,5 hari)
Inti produk. Pindahkan **bersamaan** — ketiganya saling merujuk di hampir setiap permintaan. Sekaligus satukan kuota ganda di atas.
Verifikasi: `cmd/bolt-verify` menghitung kunci per bucket **sebelum** migrasi; jumlahnya harus cocok dengan `SELECT count(*)` sesudahnya.

**4. Agent + tool + custom prompt** (~1 hari)
Bergantung pada tenant, jadi harus setelah langkah 3.

**5. Usage + audit event** (~1 hari)
Volume terbesar, tapi `events/store_postgres.go` dan `audit/store_postgres.go` **sudah ada** — jalur Postgres sudah tertulis. Sebagian besar pekerjaannya memindahkan data lama dan mematikan jalur bbolt.

**6. Antrean email** (~0,5 hari)
Terakhir, dan hati-hati: antrean yang hidup di dua tempat selama transisi akan mengirim email ganda ke pelanggan.

**Total ~5,5 hari kerja.**

---

## Cara tetap jalan selama transisi

Satu instance tetap jalan di sepanjang proses. Untuk tiap store:

1. Tulis implementasi Postgres di sebelah yang bbolt, **tanpa menghapus** yang lama
2. Pindahkan data dengan perintah sekali jalan, bandingkan jumlahnya
3. Ganti satu pemanggilan di `BuildApp` ke implementasi Postgres
4. Deploy, amati satu hari
5. Baru hapus store bbolt-nya

Pola ini sudah terbukti di repo Anda: `events/` dan `audit/` punya `store_bbolt.go` **dan** `store_postgres.go` berdampingan.

Rollback di tiap langkah = mengembalikan satu baris di `BuildApp`.

---

## Setelah selesai

- `bolt.Open` hilang dari `main.go` → `TRAILSEAL_DATA_DIR` tidak lagi dibutuhkan
- Deploy jadi zero-downtime; disk persisten tidak perlu lagi
- Bisa menjalankan lebih dari satu instance
- `GET /api/admin/backup/bolt` dan `cmd/bolt-verify` bisa dihapus — PITR Neon menggantikan keduanya
- `/readyz` kehilangan pemeriksaan `bolt`, menyisakan `postgres`

---

## Yang BELUM terbukti di dokumen ini

Daftar store di atas disusun dari nama berkas dan pemanggilan yang terlihat selama sesi ini, **bukan** dari pembacaan setiap berkas. Sebelum mengeksekusi, jalankan:

```powershell
Get-ChildItem -Recurse -Filter *.go |
  Where-Object { $_.Name -notmatch '_test\.go$' } |
  Select-String -Pattern 'bolt\.DB|bbolt' |
  ForEach-Object { "$($_.Path.Replace($PWD.Path,'')):$($_.LineNumber)" }
```

dan cocokkan hasilnya dengan tabel di atas. Store yang terlewat akan menahan file lock, dan satu saja cukup untuk membatalkan seluruh manfaat migrasi.
