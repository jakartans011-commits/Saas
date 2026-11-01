package auth

import (
	"strings"
	"sync"
	"time"
)

// ============================================================
// BATAS UKURAN PENAMPUNG
//
// Ketiga limiter di berkas ini menyimpan satu record per kunci di
// memori, dan ketiganya hanya menyusut lewat jalur yang menyentuh SATU
// kunci: saat kunci yang sama diperiksa atau dicatat ulang, atau saat
// login berhasil. Kunci yang tidak pernah muncul lagi tinggal selamanya.
//
// Kuncinya adalah IP dan alamat email — keduanya ditentukan oleh
// pemanggil, bukan oleh jumlah pelanggan. Jadi penampungnya tumbuh
// seiring jumlah percobaan dari luar, bukan seiring pemakaian yang sah,
// dan siapa pun bisa menumbuhkannya tanpa akun.
//
// Penyapuan hanya berjalan saat penampung sudah melewati ambang,
// sehingga login normal tidak pernah membayar biaya O(n). Ambangnya
// disesuaikan dengan berapa lama record hidup di masing-masing limiter.
// ============================================================

const (
	credentialStuffingSweepThreshold = 4096
	accountLockoutSweepThreshold     = 8192
	sessionRefreshSweepThreshold     = 4096
)

// ============================================================
// CREDENTIAL STUFFING / IP LOGIN LIMITER
//
// Berbeda dengan LoginAttemptLimiter yang menggunakan:
//
//	IP + email
//
// limiter ini menggunakan IP SAJA.
//
// Tujuannya: attacker tidak dapat menghindari brute-force
// protection dengan mencoba banyak email berbeda dari IP yang sama.
// ============================================================

const (
	CredentialStuffingWindow      = time.Minute
	CredentialStuffingMaxAttempts = 30
)

type CredentialStuffingLimiter struct {
	mu      sync.Mutex
	records map[string]*credentialStuffingRecord
}

type credentialStuffingRecord struct {
	attempts  int
	windowEnd time.Time
}

func NewCredentialStuffingLimiter() *CredentialStuffingLimiter {
	return &CredentialStuffingLimiter{
		records: make(map[string]*credentialStuffingRecord),
	}
}

// Allow menentukan apakah IP masih boleh melakukan login.
func (l *CredentialStuffingLimiter) Allow(ip string) bool {
	key := strings.TrimSpace(ip)
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

	return rec.attempts < CredentialStuffingMaxAttempts
}

// RecordAttempt mencatat satu percobaan login dari IP.
func (l *CredentialStuffingLimiter) RecordAttempt(ip string) {
	key := strings.TrimSpace(ip)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Satu-satunya jalur yang menambah record.
	if len(l.records) >= credentialStuffingSweepThreshold {
		l.sweepExpiredLocked(now)
	}

	rec, ok := l.records[key]

	if !ok || now.After(rec.windowEnd) {
		l.records[key] = &credentialStuffingRecord{
			attempts:  1,
			windowEnd: now.Add(CredentialStuffingWindow),
		}
		return
	}

	rec.attempts++
}

// sweepExpiredLocked membuang record yang window-nya sudah lewat.
// Pemanggil wajib memegang l.mu.
func (l *CredentialStuffingLimiter) sweepExpiredLocked(now time.Time) {
	for k, rec := range l.records {
		if now.After(rec.windowEnd) {
			delete(l.records, k)
		}
	}
}

// ============================================================
// ACCOUNT LOCKOUT
//
// Counter dikaitkan ke email/account, bukan IP.
//
// 10 kegagalan dalam 10 menit:
//
//	-> account locked 15 menit
//
// Successful authentication:
//
//	-> counter di-reset.
//
// Lockout bersifat sementara sehingga tidak menjadi permanent
// account destruction dan dapat pulih otomatis.
// ============================================================

const (
	AccountLockoutWindow   = 10 * time.Minute
	AccountLockoutDuration = 15 * time.Minute
	AccountLockoutFailures = 10
)

type AccountLockout struct {
	mu      sync.Mutex
	records map[string]*accountLockoutRecord
}

type accountLockoutRecord struct {
	failures  int
	windowEnd time.Time
	lockedTo  time.Time
}

func NewAccountLockout() *AccountLockout {
	return &AccountLockout{
		records: make(map[string]*accountLockoutRecord),
	}
}

func (l *AccountLockout) IsLocked(email string) bool {
	key := normalizeSecurityEmail(email)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[key]
	if !ok {
		return false
	}

	if !rec.lockedTo.IsZero() {
		if now.Before(rec.lockedTo) {
			return true
		}

		// Lockout expired.
		delete(l.records, key)
		return false
	}

	if now.After(rec.windowEnd) {
		delete(l.records, key)
		return false
	}

	return false
}

// RecordFailure mencatat kegagalan password.
//
// Setelah threshold tercapai, account dikunci sementara.
func (l *AccountLockout) RecordFailure(email string) {
	key := normalizeSecurityEmail(email)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Satu-satunya jalur yang menambah record.
	if len(l.records) >= accountLockoutSweepThreshold {
		l.sweepExpiredLocked(now)
	}

	rec, ok := l.records[key]

	if !ok || now.After(rec.windowEnd) {
		l.records[key] = &accountLockoutRecord{
			failures:  1,
			windowEnd: now.Add(AccountLockoutWindow),
		}
		return
	}

	if !rec.lockedTo.IsZero() && now.Before(rec.lockedTo) {
		return
	}

	rec.failures++

	if rec.failures >= AccountLockoutFailures {
		rec.lockedTo = now.Add(AccountLockoutDuration)
	}
}

// Reset menghapus counter setelah login berhasil.
func (l *AccountLockout) Reset(email string) {
	key := normalizeSecurityEmail(email)

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.records, key)
}

// sweepExpiredLocked membuang record yang sudah tidak berguna.
// Pemanggil wajib memegang l.mu.
//
// Urutan pemeriksaannya MENIRU IsLocked dengan sengaja. Akun yang
// sedang terkunci harus tetap tinggal: membuangnya akan melepaskan
// penguncian lebih awal, yaitu memperbaiki kebocoran memori dengan
// harga membuka kembali celah brute-force.
func (l *AccountLockout) sweepExpiredLocked(now time.Time) {
	for k, rec := range l.records {
		if !rec.lockedTo.IsZero() {
			// Masih terkunci — jangan disentuh.
			if now.After(rec.lockedTo) {
				delete(l.records, k)
			}
			continue
		}

		if now.After(rec.windowEnd) {
			delete(l.records, k)
		}
	}
}

// ============================================================
// SESSION / REFRESH ABUSE LIMITER
//
// Refresh token memang harus bisa dipakai untuk mendapatkan
// access token baru, tetapi attacker tidak boleh melakukan
// refresh rotation berkali-kali dalam waktu sangat singkat.
//
// Karena setiap refresh token hanya bisa dipakai sekali,
// limiter ini merupakan defense-in-depth tambahan.
// ============================================================

const (
	SessionRefreshWindow      = 5 * time.Minute
	SessionRefreshMaxAttempts = 10
)

type SessionRefreshLimiter struct {
	mu      sync.Mutex
	records map[string]*sessionRefreshRecord
}

type sessionRefreshRecord struct {
	attempts  int
	windowEnd time.Time
}

func NewSessionRefreshLimiter() *SessionRefreshLimiter {
	return &SessionRefreshLimiter{
		records: make(map[string]*sessionRefreshRecord),
	}
}

func (l *SessionRefreshLimiter) Allow(ip, userID string) bool {
	key := sessionRefreshKey(ip, userID)
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

	return rec.attempts < SessionRefreshMaxAttempts
}

func (l *SessionRefreshLimiter) Record(ip, userID string) {
	key := sessionRefreshKey(ip, userID)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Satu-satunya jalur yang menambah record.
	if len(l.records) >= sessionRefreshSweepThreshold {
		l.sweepExpiredLocked(now)
	}

	rec, ok := l.records[key]

	if !ok || now.After(rec.windowEnd) {
		l.records[key] = &sessionRefreshRecord{
			attempts:  1,
			windowEnd: now.Add(SessionRefreshWindow),
		}
		return
	}

	rec.attempts++
}

// sweepExpiredLocked membuang record yang window-nya sudah lewat.
// Pemanggil wajib memegang l.mu.
func (l *SessionRefreshLimiter) sweepExpiredLocked(now time.Time) {
	for k, rec := range l.records {
		if now.After(rec.windowEnd) {
			delete(l.records, k)
		}
	}
}

func normalizeSecurityEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func sessionRefreshKey(ip, userID string) string {
	return strings.TrimSpace(ip) + "\x00" + strings.TrimSpace(userID)
}
