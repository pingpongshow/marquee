//go:build !(linux || darwin || freebsd)

package notify

import "errors"

func freePercent(string) (float64, error) {
	return 0, errors.New("free space isn't available on this system")
}

func deviceOf(path string) string { return path }
