package main

import (
	"bytes"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	bolt "go.etcd.io/bbolt"
)

// ============================================================
// SNAPSHOT bbolt
//
// KENAPA ENDPOINT, BUKAN SKRIP
//
// bbolt mengunci berkasnya dengan flock EKSKLUSIF selama proses server
// hidup. Proses kedua — bahkan yang membuka read-only — akan menggantung
// sampai timeout, karena kunci baca dan kunci tulis saling meniadakan.
// Artinya tidak ada skrip di luar yang bisa membaca berkas ini selagi
// layanan berjalan.
//
// Yang TAMPAK berhasil adalah menyalin berkasnya begitu saja
// (Copy-Item / cp). Itu menghasilkan berkas berukuran benar yang isinya
// bisa rusak: bbolt menulis halaman dan meta secara bertahap, jadi
// salinan mentah bisa menangkap keadaan setengah jalan. Backup seperti
// itu baru ketahuan rusak saat Anda membutuhkannya — yaitu saat semuanya
// sudah terlambat.
//
// Berkas data\saas-api.db.bak-20260919-022006 di repo ini persis contoh
// itu: 8 MB, terlihat benar, belum pernah dibuktikan bisa dibuka.
//
// tx.WriteTo di dalam transaksi BACA memberi snapshot yang konsisten
// pada satu titik waktu, tanpa memblokir penulis, tanpa menghentikan
// layanan. Ini pola yang direkomendasikan bbolt sendiri.
//
// KEAMANAN
//
// Endpoint ini mengeluarkan SELURUH database: semua tenant, user, hash
// password, hash API key, refresh token. Ia harus berada di belakang
// requireRoot (kunci master), tidak pernah di belakang JWT tenant.
// Siapa pun yang bisa memanggilnya memiliki seluruh produk Anda.
// ============================================================

// boltBackupMaxBytes membatasi snapshot yang disalurkan lewat HTTP.
//
// Snapshot dirakit di memori lebih dulu, BUKAN dialirkan langsung ke
// koneksi. Alasannya: begitu byte pertama terkirim, status HTTP sudah
// terkunci di 200 — kegagalan di tengah penulisan akan menghasilkan
// berkas terpotong yang terlihat sukses. Untuk sebuah backup, diam-diam
// terpotong adalah kegagalan terburuk yang mungkin terjadi.
//
// Dengan merakit dulu, kegagalan apa pun masih bisa dijawab 500 dan
// tidak ada berkas palsu yang tersimpan.
//
// Di atas batas ini memori jadi masalah; pakai prosedur luring
// (hentikan layanan, salin berkas, jalankan kembali).
const boltBackupMaxBytes int64 = 512 << 20 // 512 MiB

// BoltBackupHandler menyalurkan snapshot bbolt yang konsisten.
func BoltBackupHandler(db *bolt.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if db == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "bolt_not_configured",
			})
		}

		var buf bytes.Buffer
		var size int64

		err := db.View(func(tx *bolt.Tx) error {
			size = tx.Size()
			if size > boltBackupMaxBytes {
				return fmt.Errorf(
					"database %d bytes exceeds in-memory snapshot limit %d; use the offline procedure",
					size, boltBackupMaxBytes,
				)
			}
			buf.Grow(int(size))
			_, werr := tx.WriteTo(&buf)
			return werr
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "bolt_snapshot_failed",
				"message": err.Error(),
			})
		}

		// Snapshot kosong berarti sesuatu salah. Database yang sah selalu
		// punya meta page, jadi ukurannya tidak pernah nol.
		if buf.Len() == 0 {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "bolt_snapshot_empty",
			})
		}

		name := fmt.Sprintf("saas-api-%s.db", time.Now().UTC().Format("20060102-150405"))

		c.Set(fiber.HeaderContentType, "application/octet-stream")
		c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+name+`"`)
		// Dibaca skrip backup untuk memverifikasi unduhan tidak terpotong.
		c.Set("X-TrailSeal-Bolt-Size", fmt.Sprintf("%d", buf.Len()))

		return c.Send(buf.Bytes())
	}
}
