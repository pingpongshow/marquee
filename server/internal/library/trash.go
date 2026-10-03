package library

import (
	"path/filepath"
	"strings"
)

// TrashDir is the folder inside each library folder where media deleted from Library Health
// waits before it is removed for good (ADM-11). Scans and the file watcher skip it.
const TrashDir = ".marquee-trash"

// InTrash reports whether p is a trash folder or inside one.
func InTrash(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if strings.EqualFold(part, TrashDir) {
			return true
		}
	}
	return false
}
