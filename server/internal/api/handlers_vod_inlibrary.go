package api

import (
	"context"
	"log/slog"

	"marquee/internal/auth"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// "In library" marks on films and series on demand, for everyone: a title counts when a
// library the user can see has it.

// vodInLibrary is the index of titles the library has, and which libraries the user sees.
// The index is nil (nothing marked) when it can't be built.
func (h *Handlers) vodInLibrary(ctx context.Context, s auth.Session) (*vodsave.LibraryIndex, vodsave.Visibility) {
	acc := userAccess(s.User)
	vis := vodsave.Visibility{LibraryIDs: acc.LibraryIDs, HideRecordings: acc.HideRecordings}
	if h.VODSaves == nil {
		return nil, vis
	}
	idx, err := h.VODSaves.InLibrary(ctx)
	if err != nil {
		slog.WarnContext(ctx, "vod in library", "err", err)
		return nil, vis
	}
	return idx, vis
}

// applyInLibraryFilter narrows a listing to the titles the library has (or doesn't).
func applyInLibraryFilter(f *vod.Filter, inLibrary *bool, series bool, idx *vodsave.LibraryIndex, vis vodsave.Visibility) {
	if inLibrary == nil {
		return
	}
	f.InLibrary = inLibrary
	f.LibraryIDs = idx.IDs(series, vis)
}

func markVodMovies(ms []VodMovie, idx *vodsave.LibraryIndex, vis vodsave.Visibility) {
	for i := range ms {
		ms[i].InLibrary = ptr(idx.Has(false, ms[i].Id, vis))
	}
}

func markVodSeries(ss []VodSeries, idx *vodsave.LibraryIndex, vis vodsave.Visibility) {
	for i := range ss {
		ss[i].InLibrary = ptr(idx.Has(true, ss[i].Id, vis))
	}
}
