package main

import (
	"errors"
	"testing"
)

type captured struct {
	level string
	msg   string
}

func fakeUsage(totalGB, freeGB float64) diskUsageFunc {
	const gb = 1024 * 1024 * 1024
	return func(string) (diskUsage, error) {
		return diskUsage{
			TotalBytes: uint64(totalGB * gb),
			FreeBytes:  uint64(freeGB * gb),
		}, nil
	}
}

// TestDiskMonitor_Thresholds — satu tabel untuk seluruh perilaku ambang.
//
// Ambangnya yang penting di sini, bukan angka persentasenya: alert yang
// berbunyi terlalu awal akan diabaikan, dan alert yang berbunyi saat disk
// sudah penuh datang setelah layanan mati.
func TestDiskMonitor_Thresholds(t *testing.T) {
	cases := []struct {
		name      string
		totalGB   float64
		freeGB    float64
		wantLevel string // "" = tidak boleh ada alert
	}{
		{"setengah penuh — diam", 100, 50, ""},
		{"79% — masih diam", 100, 21, ""},
		{"80% — peringatan", 100, 20, "warning"},
		{"85% — peringatan", 100, 15, "warning"},
		{"90% — kritis", 100, 10, "critical"},
		{"98% — kritis", 100, 2, "critical"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []captured
			notify := func(level, msg string, _ map[string]string) {
				got = append(got, captured{level, msg})
			}

			_, err := checkDiskOnce("/data", fakeUsage(tc.totalGB, tc.freeGB), notify)
			if err != nil {
				t.Fatalf("checkDiskOnce: %v", err)
			}

			if tc.wantLevel == "" {
				if len(got) != 0 {
					t.Fatalf("tidak boleh ada alert, got %+v", got)
				}
				return
			}

			if len(got) != 1 {
				t.Fatalf("harus 1 alert, got %d: %+v", len(got), got)
			}
			if got[0].level != tc.wantLevel {
				t.Fatalf("level = %q, want %q", got[0].level, tc.wantLevel)
			}
		})
	}
}

// TestDiskMonitor_ReportsPercent — persentase yang dikembalikan harus
// benar, karena itulah yang masuk log harian dan jadi satu-satunya bukti
// monitor ini benar-benar berjalan.
func TestDiskMonitor_ReportsPercent(t *testing.T) {
	pct, err := checkDiskOnce("/data", fakeUsage(200, 50), func(string, string, map[string]string) {})
	if err != nil {
		t.Fatalf("checkDiskOnce: %v", err)
	}
	if pct < 74.9 || pct > 75.1 {
		t.Fatalf("150/200 harus 75%%, got %.2f", pct)
	}
}

// TestDiskMonitor_ErrorIsReturnedNotSwallowed — kalau disk tidak bisa
// dibaca, itu harus naik ke pemanggil. Monitor yang menelan error akan
// mencatat "0%" selamanya dan tidak pernah berbunyi.
func TestDiskMonitor_ErrorIsReturnedNotSwallowed(t *testing.T) {
	boom := errors.New("statfs: no such file or directory")
	failing := func(string) (diskUsage, error) { return diskUsage{}, boom }

	alerted := false
	_, err := checkDiskOnce("/hilang", failing, func(string, string, map[string]string) { alerted = true })

	if !errors.Is(err, boom) {
		t.Fatalf("error harus dikembalikan apa adanya, got %v", err)
	}
	if alerted {
		t.Fatal("gagal membaca disk tidak boleh memicu alert disk penuh — itu dua masalah berbeda")
	}
}

// TestDiskUsage_ZeroTotalIsNotDivideByZero — disk bertotal 0 (mis. mount
// yang gagal) tidak boleh membuat proses panic.
func TestDiskUsage_ZeroTotalIsNotDivideByZero(t *testing.T) {
	d := diskUsage{TotalBytes: 0, FreeBytes: 0}
	if got := d.usedPercent(); got != 0 {
		t.Fatalf("total 0 harus memulangkan 0, got %v", got)
	}
}
