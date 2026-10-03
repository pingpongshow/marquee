package scanner

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"marquee/internal/library"
)

var (
	videoExts = set("mkv", "mp4", "m4v", "avi", "mov", "wmv", "ts", "m2ts", "mts", "mpg", "mpeg", "webm", "flv", "ogv", "3gp", "divx")
	audioExts = set("flac", "mp3", "m4a", "aac", "ogg", "oga", "opus", "wav", "aif", "aiff", "alac", "wma", "ape", "wv", "dsf", "dff", "mka")
	subExts   = set("srt", "ass", "ssa", "vtt", "idx", "sup", "smi")
	imageExts = set("jpg", "jpeg", "png", "webp")
)

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// candidate is a media file found on disk.
type candidate struct {
	Path  string // absolute
	Root  string // library root it was found under
	Rel   string // path relative to Root, with forward slashes
	Size  int64
	MTime int64
}

// walkResult holds media files plus a per-directory listing used to find sidecars.
type walkResult struct {
	Media []candidate
	Dirs  map[string][]string // dir → file names (all files, for sidecar lookup)
}

// ignored reports whether name matches any ignore pattern (case-insensitive glob on the base name).
func ignored(name string, patterns []string) bool {
	lower := strings.ToLower(name)
	for _, p := range patterns {
		if ok, _ := path.Match(strings.ToLower(p), lower); ok {
			return true
		}
	}
	return false
}

// walk collects media files under root. wantAudio selects music files instead of video.
func walk(root string, wantAudio bool, patterns []string, out *walkResult) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // unreadable subfolder: skip it, keep scanning
		}
		name := d.Name()
		if d.IsDir() {
			// Hidden folders (the media trash among them), Synology and recycle bins.
			if p != root && (strings.HasPrefix(name, ".") || strings.EqualFold(name, library.TrashDir) ||
				strings.EqualFold(name, "@eaDir") || strings.EqualFold(name, "#recycle")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() && d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		dir := filepath.Dir(p)
		out.Dirs[dir] = append(out.Dirs[dir], name)
		e := ext(name)
		if (wantAudio && !audioExts[e]) || (!wantAudio && !videoExts[e]) || ignored(name, patterns) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if info, err = statFollow(p); err != nil || !info.Mode().IsRegular() {
				return nil
			}
		}
		rel, _ := filepath.Rel(root, p)
		out.Media = append(out.Media, candidate{
			Path: p, Root: root, Rel: filepath.ToSlash(rel),
			Size: info.Size(), MTime: info.ModTime().Unix(),
		})
		return nil
	})
}

// IsMedia reports whether a file name has a video or audio extension the scanner picks up.
func IsMedia(name string) bool {
	e := ext(name)
	return videoExts[e] || audioExts[e]
}
