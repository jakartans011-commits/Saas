package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"trailseal/alerts"
)

// ============================================================
// ALERT DISK PENUH
//
// KENAPA BUKAN ROTASI LOG
//
// saas-api menulis log ke stderr, bukan ke berkas: main.go tidak pernah
// memanggil core.NewLogger, dan LOG_FILE/LOG_LEVEL adalah konfigurasi
// mati untuk binary ini. Di container itu justru benar — platform
// (Render, Docker) menangkap stdout/stderr dan menyimpannya di luar disk
// aplikasi. Menambahkan rotasi berkas di sini akan memindahkan log
// KEMBALI ke disk yang kita coba jaga.
//
// Sumber disk penuh yang sebenarnya ada dua:
//
//  1. bbolt tumbuh dan TIDAK PERNAH mengecil. Menghapus data membebaskan
//     halaman untuk dipakai ulang di dalam berkas, tapi ukuran berkas di
//     disk tetap. Setelah lonjakan trafik, ia tidak akan kembali kecil.
//  2. Berkas backup menumpuk kalau retensi tidak diatur.
//
// Keduanya tumbuh perlahan dan diam. Tanpa alert, gejala pertama yang
// Anda lihat adalah bbolt gagal menulis — dan seluruh layanan mati.
// ============================================================

const (
	diskCheckInterval  = 1 * time.Hour
	diskWarnPercent    = 80.0
	diskCritPercent    = 90.0
	diskCheckStartWait = 1 * time.Minute
)

// diskUsage — hasil pemeriksaan satu path.
type diskUsage struct {
	TotalBytes uint64
	FreeBytes  uint64
}

func (d diskUsage) usedPercent() float64 {
	if d.TotalBytes == 0 {
		return 0
	}
	used := d.TotalBytes - d.FreeBytes
	return float64(used) / float64(d.TotalBytes) * 100
}

// diskUsageFunc — seam untuk test. Produksi memakai implementasi
// per-OS di disk_space_unix.go / disk_space_windows.go.
type diskUsageFunc func(path string) (diskUsage, error)

// checkDiskOnce memeriksa satu path dan memberi alert bila perlu.
//
// Mengembalikan persentase terpakai supaya pemanggil (dan test) bisa
// memeriksanya tanpa menebak dari log.
func checkDiskOnce(path string, usage diskUsageFunc, notify func(level, msg string, fields map[string]string)) (float64, error) {
	du, err := usage(path)
	if err != nil {
		return 0, err
	}

	pct := du.usedPercent()
	freeGB := float64(du.FreeBytes) / (1024 * 1024 * 1024)

	fields := map[string]string{
		"path":         path,
		"terpakai_pct": fmt.Sprintf("%.1f", pct),
		"sisa_gb":      fmt.Sprintf("%.2f", freeGB),
	}

	switch {
	case pct >= diskCritPercent:
		notify("critical", fmt.Sprintf(
			"Disk %.1f%% terpakai, sisa %.2f GB. bbolt akan GAGAL MENULIS saat penuh "+
				"dan seluruh layanan mati. Bebaskan ruang sekarang.", pct, freeGB), fields)

	case pct >= diskWarnPercent:
		notify("warning", fmt.Sprintf(
			"Disk %.1f%% terpakai, sisa %.2f GB. Periksa ukuran bbolt dan berkas "+
				"backup lama.", pct, freeGB), fields)
	}

	return pct, nil
}

// alertDisk — notifier produksi.
func alertDisk(level, msg string, fields map[string]string) {
	log.Printf("DISK %s: %s (%v)", level, msg, fields)

	// dedupeKey menyertakan level supaya peringatan 80% tidak membungkam
	// yang 90%: keduanya kejadian berbeda, dan yang kedua jauh lebih
	// mendesak. alerts.NotifyCritical punya cooldown bawaan, jadi ini
	// tidak akan membanjiri kanal alert.
	alerts.NotifyCritical(
		"disk-"+level,
		"Disk hampir penuh",
		msg,
		fields,
	)
}

// startDiskMonitor menjalankan pemeriksaan berkala.
//
// Mengikuti pola startRetentionScheduler: ctx + cancel + done + stop
// bertimeout, bukan pola channel di grace sweeper.
func startDiskMonitor() (func(), error) {
	// Path yang dipantau adalah tempat bbolt hidup — itu yang mati lebih
	// dulu saat disk penuh, dan itu yang membawa seluruh layanan.
	path := os.Getenv("TRAILSEAL_DATA_DIR")
	if path == "" {
		path = "data"
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("disk monitor: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		timer := time.NewTimer(diskCheckStartWait)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				pct, err := checkDiskOnce(path, platformDiskUsage, alertDisk)
				if err != nil {
					log.Printf("disk monitor: gagal membaca %s: %v", path, err)
				} else {
					log.Printf("disk monitor: %s terpakai %.1f%%", path, pct)
				}
				timer.Reset(diskCheckInterval)
			}
		}
	}()

	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			log.Printf("disk monitor: stop timed out")
		}
	}
	return stop, nil
}
