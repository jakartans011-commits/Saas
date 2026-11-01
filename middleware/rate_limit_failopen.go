package middleware

import (
	"log"

	"trailseal/alerts"
)

// ============================================================
// KEGAGALAN PENYIMPANAN RATE LIMITER — GAGAL TERBUKA, TAPI BERSUARA
//
// Saat penyimpanan limiter error, handler meloloskan request
// (rate_limit.go, cabang "Fail-open"). Itu pilihan yang bisa dibela:
// bbolt bermasalah tidak boleh membuat seluruh API mati.
//
// Yang TIDAK bisa dibela adalah melakukannya tanpa suara.
//
// bbolt mengunci berkasnya secara eksklusif, dan repo ini punya bukti
// kunci bisa tertinggal: backend.db.lock dan dashboard.db.lock pernah
// ditemukan di data/ setelah proses mati tidak bersih. Dalam keadaan itu
// SETIAP request lolos tanpa dibatasi, tanpa satu baris pun log — rate
// limiting Anda lenyap dan tidak ada yang memberi tahu.
//
// Untuk sistem yang dijalankan tanpa pengawasan, kegagalan yang diam
// adalah kelas kegagalan paling berbahaya.
//
// alerts.NotifyCritical punya cooldown bawaan (ALERT_COOLDOWN_SECONDS),
// jadi memanggilnya per request tidak akan membanjiri kanal alert.
// ============================================================

// rateLimiterStorageFailureHook — seam untuk test.
//
// Tanpa ini, satu-satunya cara memverifikasi jalur ini adalah membaca
// kodenya dan berharap. Variabel ini membuat perilakunya bisa dibuktikan.
var rateLimiterStorageFailureHook = defaultRateLimiterStorageFailure

func defaultRateLimiterStorageFailure(err error) {
	log.Printf(
		"RATE LIMITER GAGAL-TERBUKA: penyimpanan error, request diloloskan TANPA dibatasi: %v",
		err,
	)

	alerts.NotifyCritical(
		"rate-limiter-storage-failure",
		"Rate limiter gagal-terbuka",
		"Penyimpanan rate limiter tidak bisa diakses, jadi request diloloskan "+
			"TANPA batas. Periksa berkas bbolt dan kunci yang tertinggal di data/. "+
			"Selama keadaan ini berlangsung, tidak ada perlindungan terhadap loop "+
			"klien yang salah.",
		map[string]string{
			"error": err.Error(),
		},
	)
}

// notifyRateLimiterStorageFailure dipanggil dari handler limiter.
func notifyRateLimiterStorageFailure(err error) {
	if err == nil {
		return
	}
	rateLimiterStorageFailureHook(err)
}
