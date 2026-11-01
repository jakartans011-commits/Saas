package auth

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================
// PENYAPUAN LoginAttemptLimiter
//
// Map records hanya menyusut lewat tiga jalur yang semuanya menyentuh
// satu kunci: Allow, RecordFailure, dan Reset. Kunci yang tidak pernah
// dicoba lagi tinggal selamanya.
//
// Karena kuncinya IP+email, itu bukan kebocoran yang tumbuh pelan
// seiring jumlah pelanggan — itu kebocoran yang bisa dipicu siapa pun
// dari luar, tanpa akun, dengan mengirim percobaan login memakai email
// acak dari IP acak.
//
//	go test ./auth -run LoginAttemptLimiter_Sweep -v
// ============================================================

func TestLoginAttemptLimiter_SweepsExpiredRecordsOnGrowth(t *testing.T) {
	l := NewLoginAttemptLimiter()

	// Tiru jejak serangan: banyak kombinasi IP+email yang masing-masing
	// gagal sekali, lalu tidak pernah muncul lagi. Window-nya sudah lewat,
	// jadi tidak satu pun dari record ini masih berguna.
	expired := time.Now().Add(-2 * LoginAttemptWindow)
	for i := 0; i < loginAttemptSweepThreshold+100; i++ {
		key := loginAttemptKey(
			fmt.Sprintf("203.0.113.%d", i%256),
			fmt.Sprintf("korban%d@example.com", i),
		)
		l.records[key] = &loginAttemptRecord{
			failures:  1,
			windowEnd: expired,
		}
	}

	before := len(l.records)

	// Satu percobaan gagal yang sah. Inilah yang memicu penyapuan.
	l.RecordFailure("198.51.100.7", "asli@example.com")

	after := len(l.records)

	if after >= before {
		t.Fatalf(
			"map tidak menyusut: sebelum=%d sesudah=%d.\n"+
				"Semua record yang ditanam sudah kedaluwarsa dan seharusnya dibuang. "+
				"Tanpa penyapuan, penyerang yang mengirim percobaan login dengan "+
				"email acak dari IP acak menumbuhkan map ini tanpa batas — tanpa "+
				"akun, tanpa perlu lolos autentikasi.",
			before, after,
		)
	}

	// Yang tersisa hanya record baru tadi.
	if after != 1 {
		t.Errorf("tersisa %d record, harusnya 1 (hanya percobaan yang baru saja dicatat)", after)
	}
}

func TestLoginAttemptLimiter_SweepKeepsLiveRecords(t *testing.T) {
	l := NewLoginAttemptLimiter()

	// Separuh kedaluwarsa, separuh masih dalam window.
	expired := time.Now().Add(-2 * LoginAttemptWindow)
	live := time.Now().Add(LoginAttemptWindow)

	const liveCount = 50
	for i := 0; i < loginAttemptSweepThreshold; i++ {
		l.records[loginAttemptKey("203.0.113.1", fmt.Sprintf("lama%d@example.com", i))] =
			&loginAttemptRecord{failures: 1, windowEnd: expired}
	}
	for i := 0; i < liveCount; i++ {
		l.records[loginAttemptKey("203.0.113.2", fmt.Sprintf("aktif%d@example.com", i))] =
			&loginAttemptRecord{failures: 3, windowEnd: live}
	}

	l.RecordFailure("198.51.100.7", "asli@example.com")

	// liveCount yang masih aktif + 1 yang baru dicatat.
	if got := len(l.records); got != liveCount+1 {
		t.Fatalf("tersisa %d record, harusnya %d — penyapuan tidak boleh membuang "+
			"record yang window-nya belum lewat, karena itu akan menghapus "+
			"hitungan kegagalan yang sedang menahan brute-force.",
			got, liveCount+1)
	}

	// Dan penguncian brute-force yang sedang berjalan harus tetap berlaku.
	for i := 0; i < liveCount; i++ {
		email := fmt.Sprintf("aktif%d@example.com", i)
		rec, ok := l.records[loginAttemptKey("203.0.113.2", email)]
		if !ok {
			t.Fatalf("record aktif untuk %s hilang setelah penyapuan", email)
		}
		if rec.failures != 3 {
			t.Errorf("record aktif untuk %s: failures=%d, harusnya 3", email, rec.failures)
		}
	}
}

func TestLoginAttemptLimiter_NoSweepBelowThreshold(t *testing.T) {
	l := NewLoginAttemptLimiter()

	// Di bawah ambang, penyapuan tidak berjalan sama sekali — login
	// normal tidak boleh membayar biaya O(n) di jalur panasnya.
	expired := time.Now().Add(-2 * LoginAttemptWindow)
	for i := 0; i < 10; i++ {
		l.records[loginAttemptKey("203.0.113.1", fmt.Sprintf("lama%d@example.com", i))] =
			&loginAttemptRecord{failures: 1, windowEnd: expired}
	}

	l.RecordFailure("198.51.100.7", "asli@example.com")

	if got := len(l.records); got != 11 {
		t.Errorf("tersisa %d record, harusnya 11 — tidak ada penyapuan di bawah ambang", got)
	}
}
