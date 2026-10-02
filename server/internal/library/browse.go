package library

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrOutsideRoots = errors.New("path is outside the allowed browse roots")
	ErrNoSuchDir    = errors.New("directory not found")
)

type DirEntry struct{ Name, Path string }

// Browse lists subdirectories of dir for the folder picker. dir must resolve (after symlinks)
// to a location inside one of roots. An empty dir lists the roots themselves.
// It returns the parent directory, or "" when dir is a root.
func Browse(roots []string, dir string) (entries []DirEntry, parent string, err error) {
	if dir == "" {
		for _, r := range roots {
			if fi, err := os.Stat(r); err == nil && fi.IsDir() {
				entries = append(entries, DirEntry{Name: r, Path: r})
			}
		}
		return entries, "", nil
	}
	if !filepath.IsAbs(dir) {
		return nil, "", ErrOutsideRoots
	}
	dir = filepath.Clean(dir)
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, "", ErrNoSuchDir
	}
	root, ok := containingRoot(roots, real)
	if !ok {
		return nil, "", ErrOutsideRoots
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", ErrNoSuchDir
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, de.Name())
		if fi, err := os.Stat(full); err == nil && fi.IsDir() { // follows symlinks to dirs
			entries = append(entries, DirEntry{Name: de.Name(), Path: full})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	if real != root {
		parent = filepath.Dir(dir)
	}
	return entries, parent, nil
}

func containingRoot(roots []string, p string) (string, bool) {
	for _, r := range roots {
		rr, err := filepath.EvalSymlinks(filepath.Clean(r))
		if err != nil {
			continue
		}
		if p == rr || strings.HasPrefix(p, rr+string(filepath.Separator)) {
			return rr, true
		}
	}
	return "", false
}
