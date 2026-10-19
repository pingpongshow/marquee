//go:build linux || darwin || freebsd

package storage

import (
	"fmt"
	"syscall"
)

// diskUsage is the size of the file system holding path and the space ordinary users can
// still write to.
func diskUsage(path string) (total, free int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := int64(st.Bsize)
	return int64(st.Blocks) * bs, int64(st.Bavail) * bs, nil
}

// deviceOf identifies the file system holding path (the path itself when unknown).
func deviceOf(path string) string {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return path
	}
	return fmt.Sprint(st.Dev)
}
