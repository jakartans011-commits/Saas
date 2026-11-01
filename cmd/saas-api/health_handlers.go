package main

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	bolt "go.etcd.io/bbolt"
)

// ============================================================
// HEALTH & READINESS
//
// Dua endpoint dengan pertanyaan yang BERBEDA. Membedakannya penting:
// kalau platform hosting me-restart proses setiap kali database sedang
// lambat, satu gangguan Postgres sesaat berubah jadi restart loop.
//
//	/healthz — "proses ini hidup?"
//	           Tidak menyentuh apa pun. Dipakai hosting untuk liveness
//	           probe: gagal = proses macet = restart.
//
//	/readyz  — "proses ini SIAP melayani request?"
//	           Memeriksa Postgres dan bbolt benar-benar bisa diakses.
//	           Gagal = jangan kirim trafik ke sini, TAPI jangan restart.
//	           Ini yang dipantau dari luar (UptimeRobot/BetterStack),
//	           karena inilah yang berkorelasi dengan "pelanggan bisa
//	           memakai produk".
//
// Keduanya TANPA autentikasi, di luar rate limiter, dan di luar audit
// log. Alasannya praktis: pemantau eksternal tidak punya kredensial,
// dan satu ketukan per menit selama 30 hari akan membanjiri audit log
// dengan 43.200 baris yang tidak punya nilai forensik apa pun.
//
// /health yang lama SENGAJA dipertahankan apa adanya — ia sudah dipakai
// test dan konfigurasi lain (lihat agent_test.go). Menghapusnya bukan
// bagian dari pekerjaan ini.
// ============================================================

// readyzCheckTimeout menjaga /readyz tetap cepat menjawab.
//
// Tanpa batas waktu, Postgres yang menggantung (bukan menolak) membuat
// endpoint ini ikut menggantung — dan pemantau eksternal akan melaporkan
// "timeout" alih-alih "not ready", dua hal yang perlu dibedakan saat
// jam 3 pagi.
const readyzCheckTimeout = 2 * time.Second

var errBoltCheckTimeout = errors.New("bolt read transaction timed out")

// HealthzHandler — liveness. Sengaja tidak menyentuh database.
func HealthzHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// ReadyzHandler — readiness. Memeriksa setiap penyimpanan yang dibutuhkan
// untuk melayani request, dan melaporkan hasil PER KOMPONEN.
//
// Laporan per komponen itu disengaja: "not_ready" saja tidak memberi tahu
// apakah yang mati Postgres atau bbolt, dan itu perbedaan antara "tunggu
// Neon pulih" dan "prosesnya kehilangan file lock-nya".
func ReadyzHandler(db *bolt.DB, pgPool *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		checks := fiber.Map{}
		ready := true

		// --- Postgres ---
		//
		// pgPool nil berarti build ini memang tidak dikonfigurasi dengan
		// Postgres (BuildApp menerima nil, dipakai test tanpa
		// TRAILSEAL_TEST_POSTGRES_URL) — bukan berarti Postgres mati.
		// Di produksi pgPool tidak pernah nil: main.go berhenti fatal
		// kalau koneksi gagal, dan validateProductionEnv mewajibkan
		// env-nya terisi.
		if pgPool == nil {
			checks["postgres"] = "not_configured"
		} else {
			ctx, cancel := context.WithTimeout(c.UserContext(), readyzCheckTimeout)
			err := pgPool.Ping(ctx)
			cancel()
			if err != nil {
				checks["postgres"] = "error: " + err.Error()
				ready = false
			} else {
				checks["postgres"] = "ok"
			}
		}

		// --- bbolt ---
		//
		// bbolt nil TIDAK ditoleransi: seluruh data tenant, user, role,
		// dan API key ada di sana. Tanpa itu proses tidak bisa melayani
		// apa pun, jadi melaporkan "ready" akan berbohong.
		if db == nil {
			checks["bolt"] = "not_configured"
			ready = false
		} else if err := boltReadable(db); err != nil {
			checks["bolt"] = "error: " + err.Error()
			ready = false
		} else {
			checks["bolt"] = "ok"
		}

		status := fiber.StatusOK
		state := "ready"
		if !ready {
			status = fiber.StatusServiceUnavailable
			state = "not_ready"
		}

		return c.Status(status).JSON(fiber.Map{
			"status": state,
			"checks": checks,
		})
	}
}

// boltReadable membuktikan file bbolt benar-benar bisa dibaca, bukan
// sekadar bahwa pointer-nya tidak nil.
//
// Transaksi BACA dipilih dengan sengaja: di bbolt pembaca tidak pernah
// memblokir penulis maupun sebaliknya, jadi pemeriksaan ini tidak bisa
// memperlambat trafik yang sedang berjalan. Transaksi TULIS akan
// mengantre di belakang setiap penulis lain — pemeriksaan kesehatan yang
// justru menambah beban saat sistem sedang sibuk.
//
// Dijalankan di goroutine dengan batas waktu karena db.Close() yang
// bersamaan, atau file yang hilang di bawah mmap, bisa membuat View
// menggantung alih-alih mengembalikan error.
func boltReadable(db *bolt.DB) error {
	done := make(chan error, 1)

	go func() {
		done <- db.View(func(tx *bolt.Tx) error {
			// Menyentuh tx memaksa mmap benar-benar dibaca; tanpa ini
			// transaksi kosong bisa lolos walau file-nya bermasalah.
			_ = tx.Size()
			return nil
		})
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(readyzCheckTimeout):
		return errBoltCheckTimeout
	}
}
