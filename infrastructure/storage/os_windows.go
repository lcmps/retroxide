//go:build windows

package storage

import (
	"errors"
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeSpace returns the bytes available to this process on path's volume.
func freeSpace(path string) (int64, bool) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	var available uint64
	if result, _, _ := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&available)), 0, 0); result == 0 {
		return 0, false
	}
	return int64(available), true
}

const (
	errorHandleDiskFull syscall.Errno = 39
	errorDiskFull       syscall.Errno = 112
)

func isDiskFull(err error) bool {
	return errors.Is(err, errorDiskFull) || errors.Is(err, errorHandleDiskFull)
}

// syncFolder is a no-op: Windows cannot open folders for flushing, and NTFS journals renames.
func syncFolder(string) error { return nil }
