//go:build !windows

package storage

import (
	"errors"
	"os"
	"syscall"
)

// freeSpace returns the bytes available to this process on path's filesystem.
func freeSpace(path string) (int64, bool) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, false
	}
	return int64(stats.Bavail) * int64(stats.Bsize), true //nolint:unconvert // field types vary by platform
}

func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}

// syncFolder flushes a folder's entries, so a rename into it survives a power cut.
func syncFolder(path string) error {
	folder, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := folder.Sync()
	return errors.Join(syncErr, folder.Close())
}
