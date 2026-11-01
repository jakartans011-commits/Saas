package auth

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func newTestRefreshStore(t *testing.T) *BoltRefreshTokenStore {
	t.Helper()
	db, err := bolt.Open(
		filepath.Join(t.TempDir(), "refresh_test.db"),
		0600,
		&bolt.Options{Timeout: time.Second},
	)
	if err != nil {
		t.Fatalf("open bolt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewBoltRefreshTokenStore(db)
	if err != nil {
		t.Fatalf("NewBoltRefreshTokenStore: %v", err)
	}
	return store
}

func seedRefreshToken(t *testing.T, store *BoltRefreshTokenStore, id, hash string, expiresAt time.Time) {
	t.Helper()
	if err := store.Save(&RefreshTokenRecord{
		ID:        id,
		UserID:    "user-1",
		TokenHash: hash,
		ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// Menggantikan TestRefreshToken_Expired_Rejected yang di-skip dengan alasan
// "requires time manipulation". Tidak ada manipulasi waktu yang dibutuhkan:
// cukup menyimpan record yang ExpiresAt-nya sudah lewat.
func TestBoltRefreshTokenStore_RotateRejectsExpired(t *testing.T) {
	store := newTestRefreshStore(t)
	seedRefreshToken(t, store, "rt-expired", "hash-expired", time.Now().Add(-time.Hour))

	if _, err := store.Rotate("hash-expired"); err == nil {
		t.Fatal("Rotate menerima token kedaluwarsa; seharusnya ditolak")
	}
}

func TestBoltRefreshTokenStore_RotateRejectsReuse(t *testing.T) {
	store := newTestRefreshStore(t)
	seedRefreshToken(t, store, "rt-reuse", "hash-reuse", time.Now().Add(time.Hour))

	if _, err := store.Rotate("hash-reuse"); err != nil {
		t.Fatalf("rotasi pertama seharusnya berhasil: %v", err)
	}
	if _, err := store.Rotate("hash-reuse"); err == nil {
		t.Fatal("Rotate menerima token yang sudah dipakai; seharusnya ditolak")
	}
}

// Membuktikan klaim atomisitas yang selama ini hanya tertulis di komentar
// Rotate: satu refresh token tidak boleh mencetak dua sesi, berapa pun
// banyaknya request paralel yang mencoba menukarkannya.
func TestBoltRefreshTokenStore_RotateIsSingleUseUnderConcurrency(t *testing.T) {
	store := newTestRefreshStore(t)
	seedRefreshToken(t, store, "rt-race", "hash-race", time.Now().Add(time.Hour))

	const attempts = 16
	var wg sync.WaitGroup
	results := make(chan error, attempts)
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.Rotate("hash-race")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("rotasi berhasil %d kali dari %d percobaan paralel; harus tepat 1", success, attempts)
	}
}
