//go:build linux || darwin || freebsd

package notify

import (
	"fmt"
	"syscall"
)

// freePercent is the share of the file system holding path that ordinary users can still
// write to.
func freePercent(path string) (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	if st.Blocks == 0 {
		return 0, fmt.Errorf("%s: empty file system", path)
	}
	return float64(st.Bavail) / float64(st.Blocks) * 100, nil
}

// deviceOf identifies the file system holding path (the path itself when unknown).
func deviceOf(path string) string {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return path
	}
	return fmt.Sprint(st.Dev)
}
