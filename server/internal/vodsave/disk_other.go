//go:build !(linux || darwin || freebsd)

package vodsave

import "errors"

func freeBytes(string) (uint64, error) { return 0, errors.New("free space unknown") }
