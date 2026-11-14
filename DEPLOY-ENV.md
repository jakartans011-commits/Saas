# TrailSeal — Variabel Lingkungan Produksi

Disusun dari kode, bukan dari ingatan: `cmd/saas-api/prod_config.go`,
`cmd/saas-api/main.go`, `db/migrate.go`, `lib/api-client.ts`, `next.config.ts`,
dan seluruh `os.Getenv` di repo. Terakhir diperiksa 2 November 2026.

> **Menggantikan versi pertama dokumen ini.** Dua hal di sana salah:
> saran `COOKIE_SAMESITE=None` (tidak perlu, lihat di bawah) dan tidak adanya
> daftar variabel Vercel sama sekali.

---

## 0. Bagaimana potongannya tersambung

Ini perlu dipahami lebih dulu, karena hampir semua keputusan di bawah
mengikutinya.

**Browser pelanggan hanya pernah bicara dengan satu domain: domain frontend.**

Next.js menerima semua permintaan, lalu meneruskan sebagian ke Go lewat
rewrite di `next.config.ts`:

| Jalur | Ke mana |
|---|---|
| `/` tanpa cookie `access_token` | Go — halaman depan (`web/index.html`) |
| `/` dengan cookie | Next — dasbor |
| `/api/*`, `/login`, `/signup`, `/verify-email`, `/reset-password`, … | Go |
| `/pricing.html`, `/docs.html`, `/api-reference.html`, `/static/*` | Go |
| Sisanya | Next |

Akibatnya yang penting:

- **Tidak ada permintaan lintas situs dari browser.** Cookie sesi yang
  disetel Go lewat proxy menjadi cookie origin frontend. `COOKIE_SAMESITE=Lax`
  sudah benar dan cukup — jangan pakai `None`, itu lebih longgar tanpa alasan.
- **`NEXT_PUBLIC_API_BASE_URL` sengaja dibiarkan KOSONG.** Mengisinya dengan
  URL absolut justru memutus tiga hal sekaligus: cookie jadi lintas origin,
  CORS mulai berlaku, dan rewrite `/` yang memeriksa cookie tidak lagi melihatnya.
- **Satu pengecualian: MCP.** Claude Desktop dan Claude Code adalah program
  di komputer pelanggan, bukan kode di halaman. Mereka memanggil Go
  **langsung**, dan `/mcp/*` memang tidak ada di daftar rewrite. Karena itu
  `NEXT_PUBLIC_MCP_BASE_URL` harus berisi alamat publik **Go**, bukan domain
  frontend.

---

## 1. Render (Go) — wajib, divalidasi, deploy gagal kalau kosong

| Variabel | Nilai | Catatan |
|---|---|---|
| `APP_ENV` | `production` | Menyalakan seluruh validasi di bawah. Tanpa ini semuanya dilewati diam-diam. |
| `TRAILSEAL_DATA_DIR` | `/var/data` | **Harus absolut.** Tempat bbolt hidup; wajib sama dengan mount path disk Render. |
| `COOKIE_SECURE` | `true` | Persis string `true`. |
| `COOKIE_SAMESITE` | `Lax` | Atau kosongkan. **Jangan `None`** — lihat bagian 0. |
| `CORS_ALLOWED_ORIGINS` | `https://trailseal.dev` | Harus `https://`. Jarang terpakai karena proxy, tapi tetap divalidasi. |
| `APP_BASE_URL` | `https://trailseal.dev` | Harus `https://`. |
| `FRONTEND_BASE_URL` | `https://trailseal.dev` | Harus `https://`. Satu domain, jadi sama dengan di atas. |
| `BILLING_SUCCESS_URL` | `https://trailseal.dev/billing/success` | Harus `https://`. |
| `BILLING_CANCEL_URL` | `https://trailseal.dev/billing/cancel` | Harus `https://`. |
| `STRIPE_SECRET_KEY` | `sk_live_...` | Lihat catatan Polar di bawah. |
| `STRIPE_WEBHOOK_SECRET` | `whsec_...` | Dari endpoint webhook, bukan dari API keys. |
| `STRIPE_PRICE_PRO` | `price_...` | Tanpa ini webhook membalas "unknown Stripe price ID" dan paket pelanggan tidak pernah naik meski sudah membayar. |
| `STRIPE_PRICE_BUSINESS` | `price_...` | Sama. |
| `RESEND_API_KEY` | `re_...` | Verifikasi email dan reset password. |
| `NOTIFY_FROM_EMAIL` | `no-reply@trailseal.dev` | Domainnya harus sudah terverifikasi di Resend. |
| `ALERT_WEBHOOK_URL` | URL webhook Slack/Discord | Ke sinilah rantai hash yang putus dilaporkan. |

## 2. Render (Go) — wajib, `log.Fatal` saat startup

| Variabel | Nilai |
|---|---|
| `SAAS_JWT_SIGNING_KEY` | Acak, minimal 32 karakter. **Jangan pernah diganti setelah ada pelanggan** — mengganti berarti semua sesi putus serentak. |
| `SAAS_ROOT_API_KEY` | Acak, minimal 32 karakter. Kredensial yang memprovisi tenant. |
| `TRAILSEAL_POSTGRES_URL` | DSN Neon **produksi**. |

Membuat keduanya:

```powershell
[Convert]::ToBase64String((1..48 | ForEach-Object { Get-Random -Max 256 }))
```

## 3. Render — pengaturan service

| Variabel | Nilai |
|---|---|
| `SAAS_API_PORT` | `10000` |

Dan di pengaturan service, bukan di env:

- **Health Check Path**: `/readyz`
- **Disk**: persistent disk, mount path `/var/data` — harus sama dengan
  `TRAILSEAL_DATA_DIR`.

**Dockerfile tidak perlu menyalin folder `migrations/`.** Sejak 2 November
berkas migrasi tertanam di dalam binary lewat `go:embed`, jadi multi-stage
build yang hanya menyalin binary tetap bermigrasi dengan benar. Log akan
mencetak versi sebelum dan sesudah migrasi saat startup.

---

## 4. Vercel (Next.js) — frontend

| Variabel | Nilai | Catatan |
|---|---|---|
| `GO_API_ORIGIN` | `https://<service>.onrender.com` | **Yang paling penting di halaman ini.** Alamat Go yang dijangkau server Next. Bukan `NEXT_PUBLIC_` — nilainya tidak pernah ikut ke browser. |
| `NEXT_PUBLIC_MCP_BASE_URL` | `https://<service>.onrender.com` | Alamat publik Go untuk potongan kode Claude Desktop/Code. Kalau kosong, halaman API Keys menampilkan peringatan, bukan perintah rusak. |
| `NEXT_PUBLIC_API_BASE_URL` | **kosong** | Sengaja. Lihat bagian 0. |
| `INTERNAL_API_BASE_URL` | `https://<service>.onrender.com` | Dipakai `fetch()` sisi server, yang tidak menerima URL relatif. |
| `NEXT_PUBLIC_LOGIN_URL` | sesuai kebutuhan | Dirujuk di `lib/api-client.ts`. |

**`GO_API_ORIGIN` wajib ada SEBELUM build, bukan saat runtime.** Di Vercel
rewrite ditanam ke dalam build. Kalau ia belum terisi saat build berjalan,
seluruh `/api/*` menunjuk `http://localhost:8090` dan dasbor tidak akan
pernah menjangkau backend — padahal build-nya sukses dan tidak ada error
di mana pun. Pasang variabelnya dulu, baru deploy.

Kode sudah menjaganya sebagian: `next start` menolak jalan tanpa
`GO_API_ORIGIN`, dan build production mencetak peringatan. Di Vercel
peringatan itu ada di log build — bacalah.

---

## 5. Tidak dijaga — baca pelan-pelan

| Variabel | Default | Kapan diisi |
|---|---|---|
| `BILLING_PORTAL_RETURN_URL` | — | Ke mana pelanggan kembali dari Billing Portal. |
| `ALERT_WEBHOOK_PROVIDER` | `slack` | Isi `discord` kalau webhook-nya Discord. |
| `ALERT_COOLDOWN_SECONDS` | `300` | Jeda minimal antar alert untuk insiden yang sama. |
| `SENTRY_DSN` | — | Kosong berarti mati. **Hanya di Render, jangan di `.env.ps1` laptop.** |
| `TRAILSEAL_RELEASE` | — | Versi rilis untuk Sentry. |
| `LOG_LEVEL`, `LOG_FILE` | — | |
| `GOOGLE_OAUTH_CLIENT_ID` / `_SECRET` | — | Hanya kalau login Google ditawarkan. |
| `GITHUB_OAUTH_CLIENT_ID` / `_SECRET` | — | Hanya kalau login GitHub ditawarkan. |
| `STRIPE_RECONCILE_APPLY` | — | Rekonsiliasi langganan. |

**Kalau mengaktifkan OAuth, periksa dulu jalur callback-nya.** Daftar rewrite
di `next.config.ts` memuat `/api/*`, `/login`, `/signup`, dan beberapa lagi —
tapi tidak ada entri `/auth/*`. Kalau callback OAuth Anda hidup di luar
`/api/`, ia tidak akan diteruskan ke Go dan loginnya gagal tanpa pesan yang
jelas. Tambahkan rewrite-nya sebelum menyalakan OAuth.

### Jangan pernah diset di produksi

| Variabel | Akibat kalau terisi |
|---|---|
| `SHADOWHARVEST_TEST_BCRYPT_COST` | Biaya bcrypt turun dari 12; setiap password pelanggan jauh lebih murah dipecahkan. Sejak 2 November ditolak validasi. |
| `TRAILSEAL_TEST_POSTGRES_URL` | Hanya untuk test. Jangan pernah diarahkan ke database produksi. |

---

## 6. Catatan pembayaran

Variabel `STRIPE_*` di atas masih yang dipakai kode hari ini. Kalau Anda jadi
pindah ke Polar sebagai Merchant of Record, keempatnya diganti oleh token dan
ID produk Polar, dan `prod_config.go` ikut menyesuaikan. Struktur webhook-nya
sama — terima, verifikasi tanda tangan, naikkan paket tenant — jadi
perubahannya beberapa hari, bukan beberapa minggu.

Akun Stripe berbasis Indonesia hanya bisa menagih dalam IDR dan tidak
melayani transaksi lintas negara, jadi `STRIPE_SECRET_KEY` dari akun
Indonesia **tidak** akan bisa menagih $99 ke pelanggan luar negeri.

---

## 7. Sebelum deploy pertama

- [ ] Buat branch Neon produksi yang tidak kedaluwarsa. Branch `testing`
      kedaluwarsa **8 November 2026**.
- [ ] Hapus `SENTRY_DSN` dari `.env.ps1` di laptop — sesi pengembangan lokal
      mengirim data ke proyek Sentry produksi setiap kali server dijalankan.
- [ ] Pertimbangkan `.env.local.ps1` terpisah untuk pengembangan: `SENTRY_DSN`
      dan `ALERT_WEBHOOK_URL` dikosongkan, kunci Stripe test. Sesi lokal
      berhenti mengotori layanan produksi.
- [ ] Bersihkan data uji coba dari database produksi, atau terima bahwa
      laporan pemakaian Anda tercampur sejak hari pertama.
- [ ] `GO_API_ORIGIN` terpasang di Vercel **sebelum** build pertama.
- [ ] CI hijau di commit yang akan dideploy.

## 8. Setelah deploy pertama

- [ ] `GET /healthz` balas 200.
- [ ] `GET /readyz` balas 200 dengan seluruh `checks` hijau.
- [ ] Log startup Render memuat `migration state after migrating: version=11`.
- [ ] Log build Vercel **tidak** memuat peringatan `GO_API_ORIGIN belum di-set`.
- [ ] Buka halaman depan tanpa login — harus muncul `web/index.html` dari Go,
      bukan dasbor.
- [ ] Daftar akun baru, verifikasi email, login. Membuktikan Resend bekerja
      dan cookie sampai lewat proxy.
- [ ] Buat API key, dan **periksa ketiga tab "Connect a client"** memuat
      alamat lengkap — bukan `/mcp/v1/rpc` telanjang, bukan kotak peringatan.
- [ ] **Deploy ulang, lalu login lagi dengan akun yang sama.** Satu-satunya
      cara membuktikan `TRAILSEAL_DATA_DIR` menunjuk disk yang bertahan.
      Kalau akunnya hilang, disk Render belum terpasang.
- [ ] Kirim satu webhook uji dari penyedia pembayaran dan pastikan paket
      tenant naik.