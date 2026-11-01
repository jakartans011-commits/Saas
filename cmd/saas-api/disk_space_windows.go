//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// platformDiskUsage (Windows — hanya untuk pengembangan lokal).
//
// GetDiskFreeSpaceExW memulangkan tiga nilai; yang dipakai adalah
// freeToCaller, bukan totalFree, karena kuota per-pengguna bisa membuat
// keduanya berbeda.
func platformDiskUsage(path string) (diskUsage, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")

	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return diskUsage{}, err
	}

	var freeToCaller, total, totalFree uint64
	r, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return diskUsage{}, callErr
	}

	return diskUsage{TotalBytes: total, FreeBytes: freeToCaller}, nil
}
