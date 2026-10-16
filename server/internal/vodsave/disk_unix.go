//go:build linux || darwin || freebsd

package vodsave

import "syscall"

// freeBytes is the space ordinary users can still write on the volume holding path.
func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
