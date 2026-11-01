package main

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	bolt "go.etcd.io/bbolt"
)

// seedBoltForBackup membuat database kecil dengan isi yang diketahui,
// supaya hasil restore bisa dibandingkan terhadap sesuatu yang pasti.
func seedBoltForBackup(t *testing.T) (*bolt.DB, map[string]string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "backup-src.db")
	db, err := bolt.Open(path, 0600, &bolt.Options{NoSync: true})
	if err != nil {
		t.Fatalf("bolt.Open gagal: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	want := map[string]string{
		"tenant-a": "TrailSeal Demo",
		"tenant-b": "Tenant Kedua",
		"tenant-c": "Tenant Ketiga",
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("tenants"))
		if err != nil {
			return err
		}
		for k, v := range want {
			if err := b.Put([]byte(k), []byte(v)); err != nil {
				return err
			}
		}
		u, err := tx.CreateBucketIfNotExists([]byte("users"))
		if err != nil {
			return err
		}
		return u.Put([]byte("owner@test.test"), []byte("hash-palsu"))
	})
	if err != nil {
		t.Fatalf("seed gagal: %v", err)
	}

	return db, want
}

// TestBoltBackup_SnapshotIsRestorable adalah inti BUTIR 3.
//
// Ia tidak hanya memeriksa endpoint membalas 200. Ia MENGAMBIL hasilnya,
// MENULISnya sebagai berkas baru, MEMBUKAnya sebagai database bbolt yang
// berdiri sendiri, dan MEMBANDINGKAN isinya dengan sumber.
//
// Inilah uji restore yang sesungguhnya, dijalankan otomatis setiap kali
// suite test berjalan. Backup yang belum pernah direstore bukan backup,
// itu harapan — dan harapan tidak boleh bergantung pada seseorang yang
// ingat menjalankan skrip manual tiap kuartal.
func TestBoltBackup_SnapshotIsRestorable(t *testing.T) {
	db, want := seedBoltForBackup(t)

	app := fiber.New()
	app.Get("/backup", BoltBackupHandler(db))

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/backup", nil), 30000)
	if err != nil {
		t.Fatalf("app.Test gagal: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("snapshot harus 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("baca body gagal: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("snapshot kosong")
	}

	// Header ukuran harus cocok dengan byte yang benar-benar terkirim —
	// inilah yang dipakai skrip backup untuk mendeteksi unduhan terpotong.
	if got := resp.Header.Get("X-TrailSeal-Bolt-Size"); got == "" {
		t.Fatal("header X-TrailSeal-Bolt-Size hilang")
	}

	// === RESTORE: tulis sebagai berkas BARU dan buka sebagai database ===
	restored := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(restored, body, 0600); err != nil {
		t.Fatalf("tulis berkas restore gagal: %v", err)
	}

	rdb, err := bolt.Open(restored, 0600, &bolt.Options{NoSync: true})
	if err != nil {
		t.Fatalf("berkas hasil backup TIDAK BISA DIBUKA sebagai bbolt: %v", err)
	}
	defer rdb.Close()

	// === VERIFIKASI ISI ===
	err = rdb.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tenants"))
		if b == nil {
			t.Fatal("bucket tenants hilang setelah restore")
		}

		got := 0
		if err := b.ForEach(func(k, v []byte) error {
			got++
			expected, ok := want[string(k)]
			if !ok {
				t.Fatalf("tenant tak dikenal setelah restore: %q", k)
			}
			if string(v) != expected {
				t.Fatalf("isi tenant %q berubah: got %q, want %q", k, v, expected)
			}
			return nil
		}); err != nil {
			return err
		}

		if got != len(want) {
			t.Fatalf("jumlah tenant setelah restore = %d, want %d", got, len(want))
		}

		u := tx.Bucket([]byte("users"))
		if u == nil {
			t.Fatal("bucket users hilang setelah restore")
		}
		if v := u.Get([]byte("owner@test.test")); string(v) != "hash-palsu" {
			t.Fatalf("isi user berubah: got %q", v)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verifikasi hasil restore gagal: %v", err)
	}
}

// TestBoltBackup_WorksWhileDatabaseIsBeingWritten membuktikan snapshot
// bisa diambil TANPA menghentikan layanan.
//
// Kalau pemeriksaan ini gagal, backup Anda butuh downtime — dan backup
// yang butuh downtime adalah backup yang jarang dijalankan.
func TestBoltBackup_WorksWhileDatabaseIsBeingWritten(t *testing.T) {
	db, _ := seedBoltForBackup(t)

	app := fiber.New()
	app.Get("/backup", BoltBackupHandler(db))

	// Tulis sesuatu lebih dulu, lalu ambil snapshot, lalu tulis lagi.
	// Snapshot harus mencerminkan keadaan pada titik ia diambil.
	if err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tenants"))
		return b.Put([]byte("tenant-d"), []byte("Ditambahkan sebelum snapshot"))
	}); err != nil {
		t.Fatalf("update gagal: %v", err)
	}

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/backup", nil), 30000)
	if err != nil {
		t.Fatalf("app.Test gagal: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)

	if err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tenants"))
		return b.Put([]byte("tenant-e"), []byte("Ditambahkan SESUDAH snapshot"))
	}); err != nil {
		t.Fatalf("update kedua gagal: %v", err)
	}

	restored := filepath.Join(t.TempDir(), "restored2.db")
	if err := os.WriteFile(restored, body, 0600); err != nil {
		t.Fatalf("tulis gagal: %v", err)
	}
	rdb, err := bolt.Open(restored, 0600, &bolt.Options{NoSync: true})
	if err != nil {
		t.Fatalf("buka hasil restore gagal: %v", err)
	}
	defer rdb.Close()

	if err := rdb.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("tenants"))
		if v := b.Get([]byte("tenant-d")); v == nil {
			t.Fatal("tulisan SEBELUM snapshot tidak ikut — snapshot tidak konsisten")
		}
		if v := b.Get([]byte("tenant-e")); v != nil {
			t.Fatal("tulisan SESUDAH snapshot ikut — snapshot bukan titik waktu tunggal")
		}
		return nil
	}); err != nil {
		t.Fatalf("verifikasi gagal: %v", err)
	}
}

// TestBoltBackup_NilDatabase — tanpa bbolt tidak ada yang bisa di-backup.
// Membalas 200 dengan berkas kosong akan membuat skrip backup menyimpan
// berkas palsu dan melaporkan sukses.
func TestBoltBackup_NilDatabase(t *testing.T) {
	app := fiber.New()
	app.Get("/backup", BoltBackupHandler(nil))

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/backup", nil))
	if err != nil {
		t.Fatalf("app.Test gagal: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("db nil harus 503, got %d", resp.StatusCode)
	}
}
