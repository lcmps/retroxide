//go:build linux

package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// reserve allocates size bytes for file up front, so the filesystem can place it in one run of
// blocks instead of growing it piece by piece, and a full disk is found before any is written.
// Filesystems that cannot preallocate (FAT, some network mounts) simply skip it.
//
// keepSize reserves the space without growing the file, for a file filled in later parts.
func reserve(file *os.File, size int64, keepSize bool) error {
	mode := uint32(0)
	if keepSize {
		mode = unix.FALLOC_FL_KEEP_SIZE
	}
	err := unix.Fallocate(int(file.Fd()), mode, 0, size)
	if err == nil || isDiskFull(err) {
		return err
	}
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return nil
	}
	return err
}

// dropFromCache tells the kernel file's pages need not stay in memory. A multi-gigabyte ROM
// written or read once would otherwise push the database and everything else out of the page
// cache, which on a board with 1 GB of memory is most of it. Written pages must be flushed to
// disk first, or the kernel keeps them.
func dropFromCache(file *os.File) {
	_ = unix.Fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED)
}
