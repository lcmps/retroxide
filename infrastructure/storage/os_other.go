//go:build !linux

package storage

import "os"

// reserve is a no-op: only Linux offers fallocate.
func reserve(*os.File, int64, bool) error { return nil }

// dropFromCache is a no-op: only Linux offers fadvise.
func dropFromCache(*os.File) {}
