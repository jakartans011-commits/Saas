package auth

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================
// PENYAPUAN TIGA LIMITER DI login_security.go
//
// Ketiganya dikunci oleh IP atau email — ditentukan oleh pemanggil,
// bukan oleh jumlah pelanggan. Tanpa penyapuan, siapa pun bisa
// menumbuhkan penampungnya tanpa batas dari luar, tanpa akun.
//
// Test yang paling penting di berkas ini adalah
// AccountLockout_SweepKeepsLockedAccounts: penyapuan yang ceroboh akan
// melepaskan akun yang sedang terkunci, yaitu memperbaiki kebocoran
// memori dengan harga membuka kembali celah brute-force.
//
//	go test ./auth -run Sweep -v
// ============================================================

func TestCredentialStuffingLimiter_SweepsExpiredRecords(t *testing.T) {
	l := NewCredentialStuffingLimiter()

	expired := time.Now().Add(-2 * CredentialStuffingWindow)
	for i := 0; i < credentialStuffingSweepThreshold+100; i++ {
		l.records[fmt.Sprintf("203.0.113.%d", i)] = &credentialStuffingRecord{
			attempts:  1,
			windowEnd: expired,
		}
	}

	before := len(l.records)
	l.RecordAttempt("198.51.100.7")
	after := len(l.records)

	if after >= before {
		t.Fatalf("map tidak menyusut: sebelum=%d sesudah=%d", before, after)
	}
	if after != 1 {
		t.Errorf("tersisa %d record, harusnya 1", after)
	}
}

func TestCredentialStuffingLimiter_SweepKeepsLiveRecords(t *testing.T) {
	l := NewCredentialStuffingLimiter()

	expired := time.Now().Add(-2 * CredentialStuffingWindow)
	live := time.Now().Add(CredentialStuffingWindow)

	const liveCount = 40
	for i := 0; i < credentialStuffingSweepThreshold; i++ {
		l.records[fmt.Sprintf("203.0.113.%d", i)] = &credentialStuffingRecord{
			attempts: 1, windowEnd: expired,
		}
	}
	for i := 0; i < liveCount; i++ {
		l.records[fmt.Sprintf("198.51.100.%d", i)] = &credentialStuffingRecord{
			attempts: CredentialStuffingMaxAttempts, windowEnd: live,
		}
	}

	l.RecordAttempt("192.0.2.1")

	if got := len(l.records); got != liveCount+1 {
		t.Fatalf("tersisa %d record, harusnya %d", got, liveCount+1)
	}

	// IP yang sedang diblokir harus tetap diblokir.
	if l.Allow("198.51.100.0") {
		t.Error("IP yang sudah melewati batas percobaan jadi diizinkan lagi setelah penyapuan")
	}
}

func TestAccountLockout_SweepsExpiredRecords(t *testing.T) {
	l := NewAccountLockout()

	expired := time.Now().Add(-2 * AccountLockoutWindow)
	for i := 0; i < accountLockoutSweepThreshold+100; i++ {
		l.records[normalizeSecurityEmail(fmt.Sprintf("korban%d@example.com", i))] =
			&accountLockoutRecord{failures: 1, windowEnd: expired}
	}

	before := len(l.records)
	l.RecordFailure("asli@example.com")
	after := len(l.records)

	if after >= before {
		t.Fatalf("map tidak menyusut: sebelum=%d sesudah=%d", before, after)
	}
	if after != 1 {
		t.Errorf("tersisa %d record, harusnya 1", after)
	}
}

func TestAccountLockout_SweepKeepsLockedAccounts(t *testing.T) {
	l := NewAccountLockout()

	expired := time.Now().Add(-2 * AccountLockoutWindow)
	for i := 0; i < accountLockoutSweepThreshold; i++ {
		l.records[normalizeSecurityEmail(fmt.Sprintf("lama%d@example.com", i))] =
			&accountLockoutRecord{failures: 1, windowEnd: expired}
	}

	// Satu akun yang BENAR-BENAR sedang terkunci. windowEnd-nya sudah
	// lewat — yang menahannya hanya lockedTo. Penyapuan yang hanya
	// melihat windowEnd akan melepaskannya.
	const lockedEmail = "terkunci@example.com"
	l.records[normalizeSecurityEmail(lockedEmail)] = &accountLockoutRecord{
		failures:  AccountLockoutFailures,
		windowEnd: expired,
		lockedTo:  time.Now().Add(AccountLockoutDuration),
	}

	if !l.IsLocked(lockedEmail) {
		t.Fatal("fixture salah: akun seharusnya terkunci sebelum penyapuan")
	}

	l.RecordFailure("asli@example.com")

	if !l.IsLocked(lockedEmail) {
		t.Fatal(
			"akun yang sedang terkunci dilepaskan oleh penyapuan.\n" +
				"Ini lebih buruk daripada kebocoran memori yang diperbaikinya: " +
				"penyerang yang sudah memicu penguncian tinggal membanjiri limiter " +
				"dengan email acak untuk memaksa penyapuan, lalu melanjutkan " +
				"brute-force terhadap akun yang baru saja dilepaskan.",
		)
	}
}

func TestSessionRefreshLimiter_SweepsExpiredRecords(t *testing.T) {
	l := NewSessionRefreshLimiter()

	expired := time.Now().Add(-2 * SessionRefreshWindow)
	for i := 0; i < sessionRefreshSweepThreshold+100; i++ {
		l.records[sessionRefreshKey(fmt.Sprintf("203.0.113.%d", i%256), fmt.Sprintf("user-%d", i))] =
			&sessionRefreshRecord{attempts: 1, windowEnd: expired}
	}

	before := len(l.records)
	l.Record("198.51.100.7", "user-asli")
	after := len(l.records)

	if after >= before {
		t.Fatalf("map tidak menyusut: sebelum=%d sesudah=%d", before, after)
	}
	if after != 1 {
		t.Errorf("tersisa %d record, harusnya 1", after)
	}
}

func TestSessionRefreshLimiter_SweepKeepsLiveRecords(t *testing.T) {
	l := NewSessionRefreshLimiter()

	expired := time.Now().Add(-2 * SessionRefreshWindow)
	live := time.Now().Add(SessionRefreshWindow)

	const liveCount = 25
	for i := 0; i < sessionRefreshSweepThreshold; i++ {
		l.records[sessionRefreshKey("203.0.113.1", fmt.Sprintf("lama-%d", i))] =
			&sessionRefreshRecord{attempts: 1, windowEnd: expired}
	}
	for i := 0; i < liveCount; i++ {
		l.records[sessionRefreshKey("198.51.100.2", fmt.Sprintf("aktif-%d", i))] =
			&sessionRefreshRecord{attempts: SessionRefreshMaxAttempts, windowEnd: live}
	}

	l.Record("192.0.2.1", "user-asli")

	if got := len(l.records); got != liveCount+1 {
		t.Fatalf("tersisa %d record, harusnya %d", got, liveCount+1)
	}

	if l.Allow("198.51.100.2", "aktif-0") {
		t.Error("sesi yang sudah melewati batas refresh jadi diizinkan lagi setelah penyapuan")
	}
}
