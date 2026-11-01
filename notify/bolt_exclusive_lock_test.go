package notify

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Bukti untuk P0-1 (F2): bbolt mengunci file secara eksklusif. Selama
// saas-api memegang data/saas-api.db, proses lain (mis. notify-worker)
// TIDAK bisa membukanya: bolt.Open tanpa Timeout menunggu selamanya.
//
// Karena itu antrean email harus dikuras oleh proses yang SAMA dengan
// saas-api (lihat cmd/saas-api/notify_scheduler.go), bukan binary
// terpisah yang dijadwalkan dari luar.

func TestBoltDB_SecondOpenBlocksWhileFirstIsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.db")

	first, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("first bolt.Open failed: %v", err)
	}
	defer first.Close()

	second, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err == nil {
		_ = second.Close()
		t.Fatalf("second open unexpectedly succeeded: a separate worker process could share the file")
	}
	if err != bolt.ErrTimeout {
		t.Fatalf("expected bolt.ErrTimeout, got %v", err)
	}
}
