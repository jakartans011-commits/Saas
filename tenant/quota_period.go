package tenant

import "time"

// ============================================================
// PERIODE KUOTA
//
// TrailSeal sempat punya DUA definisi "sebulan" yang berbeda:
//
//   - bbolt  (TryConsumeQuota): rolling window 30 x 24 jam sejak
//     PeriodResetAt terakhir di-set.
//   - Postgres (usage_handlers.go): bulan kalender, reset tanggal 1.
//
// Selisih antara keduanya adalah kuota yang diberikan gratis. Sebuah
// tenant Free berpaket 5.000 event bisa memakai sampai 10.000 dalam
// satu bulan kalender, karena jendela 30 harinya berakhir lebih cepat
// daripada bulannya dan penghitungnya kembali ke nol di tengah jalan.
//
// nextQuotaReset menyamakan sisi bbolt dengan sisi Postgres: keduanya
// kini reset di awal bulan kalender UTC. Rumusnya sengaja ditulis
// identik dengan cmd/saas-api/usage_handlers.go supaya perbedaan di
// masa depan ketahuan sebagai perbedaan kode, bukan perbedaan perilaku
// yang tersembunyi.
//
// Catatan transisi: tenant yang PeriodResetAt-nya sudah terlanjur
// di-set ke "sekarang + 30 hari" tetap memakai tanggal lama sampai
// jendela itu habis sekali. Reset berikutnya barulah jatuh di awal
// bulan. Jadi paling banyak ada satu periode pendek per tenant, sekali
// saja, dan arahnya menguntungkan pelanggan — bukan merugikan.
// ============================================================

func nextQuotaReset(now time.Time) time.Time {
	u := now.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
}