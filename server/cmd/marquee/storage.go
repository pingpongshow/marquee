package main

import (
	"context"

	"marquee/internal/config"
	"marquee/internal/library"
	"marquee/internal/notify"
	"marquee/internal/storage"
)

// diskFolder is a folder whose volume the disk space alert watches and the storage
// overview reports: config, transcode, recordings and every library folder.
type diskFolder struct {
	name, path string
	library    bool
}

func diskFolders(ctx context.Context, cfg *config.Config, libs *library.Store) []diskFolder {
	out := []diskFolder{{name: "Config", path: cfg.ConfigDir}, {name: "Transcode", path: cfg.TranscodeDir},
		{name: "Recordings", path: cfg.RecordingsDir}}
	if list, err := libs.List(ctx); err == nil {
		for _, l := range list {
			for _, p := range l.Paths {
				out = append(out, diskFolder{name: l.Name, path: p, library: true})
			}
		}
	}
	return out
}

// alertVolumes is the disk space alert's list ("Library Movies" for a library folder).
func alertVolumes(ctx context.Context, cfg *config.Config, libs *library.Store) []notify.Volume {
	var out []notify.Volume
	for _, f := range diskFolders(ctx, cfg, libs) {
		name := f.name
		if f.library {
			name = "Library " + name
		}
		out = append(out, notify.Volume{Name: name, Path: f.path})
	}
	return out
}

// storageFolders is the storage overview's list (a library folder is named for its library).
func storageFolders(ctx context.Context, cfg *config.Config, libs *library.Store) []storage.Folder {
	var out []storage.Folder
	for _, f := range diskFolders(ctx, cfg, libs) {
		out = append(out, storage.Folder{Name: f.name, Path: f.path})
	}
	return out
}
