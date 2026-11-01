//go:build !windows

package main

import "syscall"

// platformDiskUsage (Unix/Linux — ini yang berjalan di produksi, karena
// Dockerfile membangun di Alpine).
//
// Bavail, bukan Bfree: Bfree menghitung blok yang dicadangkan untuk root
// dan tidak bisa dipakai proses biasa. Memakainya akan melaporkan ruang
// yang tidak benar-benar tersedia untuk bbolt.
func platformDiskUsage(path string) (diskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return diskUsage{}, err
	}
	bs := uint64(st.Bsize)
	return diskUsage{
		TotalBytes: st.Blocks * bs,
		FreeBytes:  st.Bavail * bs,
	}, nil
}
