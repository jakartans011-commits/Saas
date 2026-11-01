package auth

import (
	"errors"
	"time"
)

// Sentinel refresh-token errors. Sebelumnya setiap tempat membuat error
// baru dengan teks yang sama, sehingga pemanggil tidak bisa membedakan
// "token dicuri dan dipakai ulang" dari "token tidak dikenal" tanpa
// mencocokkan string. errors.Is sekarang bekerja, termasuk lewat
// pembungkusan %w di RotateRefreshTokenForAnyUser.
var (
	ErrRefreshTokenNotFound    = errors.New("refresh_token_not_found")
	ErrRefreshTokenAlreadyUsed = errors.New("refresh_token_already_used")
	ErrRefreshTokenExpired     = errors.New("refresh_token_expired")
)

// ReuseDetectionLeeway memisahkan klien yang gagap dari token yang bocor.
//
// Penggunaan ulang dalam rentang ini dianggap pengiriman ganda yang wajar:
// retry jaringan, efek ganda di SPA, atau dua tab yang menyegarkan sesi
// bersamaan. Request-nya tetap ditolak, tetapi sesi tidak dicabut —
// mencabutnya akan melempar keluar pelanggan karena gangguan jaringan.
//
// Di atas rentang ini, pemilik sah sudah lama memegang token penggantinya,
// sehingga token lama yang muncul kembali menandakan rantai refresh bocor.
const ReuseDetectionLeeway = 10 * time.Second

// RevokeAllSessionsIfTokenReused mencabut seluruh sesi pemilik token bila
// rotateErr menandakan token yang sudah dipakai ditukarkan lagi di luar
// ReuseDetectionLeeway.
//
// Menolak satu token saja tidak cukup pada kasus pencurian: token hasil
// rotasi milik penyerang tetap hidup. Satu-satunya pemulihan yang aman
// adalah memaksa semua pihak login ulang.
//
// Mengembalikan true bila pencabutan benar-benar dilakukan. Kegagalan di
// sini tidak boleh menggagalkan response: pemanggil tetap menolak request,
// jadi hasilnya aman diabaikan.
func RevokeAllSessionsIfTokenReused(
	plaintext string,
	store RefreshTokenStore,
	rotateErr error,
) bool {
	if rotateErr == nil || !errors.Is(rotateErr, ErrRefreshTokenAlreadyUsed) {
		return false
	}

	record, err := store.FindByHash(hashToken(plaintext))
	if err != nil || record == nil || record.UserID == "" {
		return false
	}

	if record.RevokedAt != nil && time.Since(*record.RevokedAt) < ReuseDetectionLeeway {
		return false
	}

	return store.RevokeAllForUser(record.UserID) == nil
}
