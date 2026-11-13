//go:build !windows

package main

import "golang.org/x/sys/unix"

// diskUsage reports the volume holding path: total and free bytes for the user.
func diskUsage(path string) (total, free int64, ok bool) {
	var s unix.Statfs_t
	if unix.Statfs(path, &s) != nil {
		return 0, 0, false
	}
	bs := int64(s.Bsize)
	return int64(s.Blocks) * bs, int64(s.Bavail) * bs, true
}
