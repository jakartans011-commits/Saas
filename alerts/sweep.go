package alerts

import "time"

// ============================================================
// BATAS UKURAN PENAMPUNG DEDUPE
//
// notifier.lastSent menyimpan satu entri per alert key supaya satu
// insiden yang berulang ratusan kali per detik hanya mengirim satu
// webhook per cooldown. Entri berhenti berguna begitu cooldown-nya
// lewat — tapi tidak ada satu pun baris di alerts.go yang pernah
// membuangnya. Setiap key baru menambah entri permanen.
//
// Pertumbuhannya pelan dan terikat ke jumlah project serta jenis
// insiden, jadi ini yang paling jinak di antara penampung yang kami
// tutup. Tapi "pelan" pada proses yang dirancang hidup berbulan-bulan
// tetap berarti naik terus, dan sebagian alert key dibentuk dari
// konteks permintaan, bukan hanya dari ID project.
//
// Ambangnya jauh di atas jumlah kombinasi insiden x project yang wajar,
// sehingga penyapuan tidak pernah berjalan pada kondisi normal.
// ============================================================

const alertDedupeSweepThreshold = 1024

// sweepCooledLocked membuang entri yang cooldown-nya sudah lewat.
// Pemanggil wajib memegang n.mu.
//
// Entri yang MASIH dalam cooldown harus tetap tinggal: membuangnya akan
// mengembalikan badai alert yang justru dicegah penampung ini.
func (n *notifier) sweepCooledLocked(now time.Time) {
	for key, last := range n.lastSent {
		if now.Sub(last) >= n.cooldown {
			delete(n.lastSent, key)
		}
	}
}
