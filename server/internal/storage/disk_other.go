//go:build !(linux || darwin || freebsd)

package storage

import "errors"

func diskUsage(string) (int64, int64, error) {
	return 0, 0, errors.New("free space isn't available on this system")
}

func deviceOf(path string) string { return path }
