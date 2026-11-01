package auth

import (
	"strings"
	"sync"
	"time"
)

const (
	LoginAttemptWindow = time.Minute
	LoginMaxFailures   = 12

	// loginAttemptSweepThreshold — jumlah record yang memicu penyapuan.
	//
	// Tanpa ini, map hanya menyusut lewat tiga jalur yang semuanya
	// menyentuh SATU kunci saja: Allow (kalau window kunci itu lewat),
	// RecordFailure (menimpa kunci itu), dan Reset (login berhasil).
	// Kunci yang tidak pernah dicoba lagi tinggal selamanya.
	//
	// Karena kuncinya IP+email, penyerang bisa menumbuhkan map ini tanpa
	// batas tanpa perlu akun: satu juta percobaan dengan email acak dari
	// IP acak meninggalkan satu juta record, masing-masing sekitar 150
	// byte, dan tidak satu pun akan pernah dibersihkan.
	//
	// Angka 4096 dipilih jauh di atas pemakaian wajar (login gagal yang
	// benar-benar terjadi bersamaan dalam satu menit) supaya penyapuan
	// tidak pernah berjalan pada beban normal, tapi cukup rendah supaya
	// penyalahgunaan tertangkap sebelum memorinya berarti.
	loginAttemptSweepThreshold = 4096
)

type LoginAttemptLimiter struct {
	mu      sync.Mutex
	records map[string]*loginAttemptRecord
}

type loginAttemptRecord struct {
	failures  int
	windowEnd time.Time
}

func NewLoginAttemptLimiter() *LoginAttemptLimiter {
	return &LoginAttemptLimiter{
		records: make(map[string]*loginAttemptRecord),
	}
}

// Allow menentukan apakah percobaan login masih diperbolehkan.
//
// Key sengaja menggunakan IP + email:
//   - melindungi akun dari brute-force password
//   - tidak langsung mengunci akun secara permanen
//   - window otomatis berakhir setelah satu menit
func (l *LoginAttemptLimiter) Allow(ip, email string) bool {
	key := loginAttemptKey(ip, email)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[key]
	if !ok {
		return true
	}

	if now.After(rec.windowEnd) {
		delete(l.records, key)
		return true
	}

	return rec.failures < LoginMaxFailures
}

// RecordFailure mencatat kegagalan login.
func (l *LoginAttemptLimiter) RecordFailure(ip, email string) {
	key := loginAttemptKey(ip, email)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Satu-satunya jalur yang menambah record, jadi satu-satunya tempat
	// yang perlu menjaga ukurannya. Penyapuan hanya berjalan saat map
	// sudah besar, sehingga login normal tidak pernah membayar O(n).
	if len(l.records) >= loginAttemptSweepThreshold {
		l.sweepExpiredLocked(now)
	}

	rec, ok := l.records[key]

	if !ok || now.After(rec.windowEnd) {
		l.records[key] = &loginAttemptRecord{
			failures:  1,
			windowEnd: now.Add(LoginAttemptWindow),
		}
		return
	}

	rec.failures++
}

// Reset menghapus kegagalan setelah login berhasil.
func (l *LoginAttemptLimiter) Reset(ip, email string) {
	key := loginAttemptKey(ip, email)

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.records, key)
}

// sweepExpiredLocked membuang semua record yang window-nya sudah lewat.
// Pemanggil wajib memegang l.mu.
//
// Karena window-nya satu menit, hampir semua record pada serangan yang
// berkelanjutan sudah kedaluwarsa saat penyapuan berjalan. Yang tersisa
// setelahnya terbatas oleh laju permintaan dikali satu menit — dan laju
// itu sendiri sudah dibatasi rate limiter di depannya.
func (l *LoginAttemptLimiter) sweepExpiredLocked(now time.Time) {
	for k, rec := range l.records {
		if now.After(rec.windowEnd) {
			delete(l.records, k)
		}
	}
}

func loginAttemptKey(ip, email string) string {
	return strings.TrimSpace(ip) + "\x00" +
		strings.ToLower(strings.TrimSpace(email))
}
