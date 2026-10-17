package vodsave

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"marquee/internal/vod"
)

// "In library" marks: films by TMDB id, else by title and year (not one with another TMDB
// id); shows when the library has any of their episodes; only libraries the user can see
// count; the listings filter on it in one query.
func TestInLibraryIndex(t *testing.T) {
	films := `[{"name":"The Matrix (1999)","stream_id":"7","container_extension":"mkv","tmdb":"603"},
		{"name":"EN - Heat (1995)","stream_id":"8","container_extension":"mkv"},
		{"name":"Heat (1986)","stream_id":"9","container_extension":"mkv"},
		{"name":"Alien (1979)","stream_id":"10","container_extension":"mkv","tmdb":"348"},
		{"name":"Brazil (1985)","stream_id":"11","container_extension":"mkv"}]`
	e := newEnv(t, films, nil)
	e.src.series = `[{"name":"Kings (2011)","series_id":"201","category_id":"1","tmdb":"77"},
		{"name":"Empty (2015)","series_id":"202","category_id":"1"},
		{"name":"Queens (2010)","series_id":"203","category_id":"1"}]`
	ctx := context.Background()
	e.vod.Refresh(ctx, false)
	d := e.s.DB
	matrix, heat, alien := e.movieID("The Matrix (1999)"), e.movieID("EN - Heat (1995)"), e.movieID("Alien (1979)")
	d.Exec("UPDATE vod_movies SET match_state='matched' WHERE id IN (?, ?)", matrix, alien)
	seriesID := func(title string) int64 {
		var id int64
		if err := d.QueryRow("SELECT id FROM vod_series WHERE title=?", title).Scan(&id); err != nil {
			t.Fatalf("series %q: %v", title, err)
		}
		return id
	}
	kings, empty := seriesID("Kings (2011)"), seriesID("Empty (2015)")
	d.Exec("UPDATE vod_series SET match_state='matched', tmdb_id=77 WHERE id=?", kings)

	movies, shows := e.libID("Movies"), e.libID("Shows")
	e.libFile(movies, "movie", "Matrix, The", 1999, 603, filepath.Join(e.movies, "M", "m.mkv"), 1920, 1080, "hevc", 8000)
	e.libFile(movies, "movie", "Heat", 1995, 0, filepath.Join(e.movies, "Heat (1995)", "Heat.mkv"), 1280, 720, "h264", 4000)
	e.libFile(movies, "movie", "Alien", 1979, 999, filepath.Join(e.movies, "A", "a.mkv"), 1920, 1080, "h264", 8000)
	// Kings: a show with an episode (by TMDB id). Empty: a show with no episodes.
	r, _ := d.Exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES(?, 'show', 'Kings', 'Kings', 2011)`, shows)
	show, _ := r.LastInsertId()
	d.Exec(`INSERT INTO external_ids(item_id, provider, value) VALUES(?, 'tmdb', '77')`, show)
	r, _ = d.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, idx) VALUES(?, 'season', 'Season 1', 'Season 1', ?, 1)`, shows, show)
	season, _ := r.LastInsertId()
	r, _ = d.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES(?, 'episode', 'One', 'One', ?, ?, 1)`, shows, season, show)
	ep, _ := r.LastInsertId()
	r, _ = d.Exec(`INSERT INTO media_versions(item_id) VALUES(?)`, ep)
	v, _ := r.LastInsertId()
	d.Exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime) VALUES(?,?,?,1,1)`, v, shows, filepath.Join(e.shows, "k.mkv"))
	d.Exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES(?, 'show', 'Empty', 'Empty', 2015)`, shows)

	idx, err := e.s.InLibrary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all := Visibility{}
	if got := idx.IDs(false, all); !slices.Equal(got, []int64{matrix, heat}) {
		t.Errorf("films in the library: %v, want %v", got, []int64{matrix, heat})
	}
	if got := idx.IDs(true, all); !slices.Equal(got, []int64{kings}) {
		t.Errorf("series in the library: %v, want %v", got, []int64{kings})
	}
	if idx.Has(true, empty, all) {
		t.Error("a show without episodes counted")
	}
	// Only the libraries the user can see.
	if onlyShows := (Visibility{LibraryIDs: []int64{shows}}); idx.Has(false, matrix, onlyShows) || !idx.Has(true, kings, onlyShows) {
		t.Error("library access ignored")
	}
	d.Exec("UPDATE libraries SET recordings=1 WHERE id=?", movies)
	e.s.ForgetLibraryIndex()
	idx, _ = e.s.InLibrary(ctx)
	if idx.Has(false, matrix, Visibility{HideRecordings: true}) || !idx.Has(false, matrix, all) {
		t.Error("recordings libraries for a profile that can't see them")
	}

	// The listing filter.
	list := func(in bool) []string {
		f := vod.Filter{InLibrary: &in, LibraryIDs: idx.IDs(false, all)}
		ms, total, err := e.vod.Movies(ctx, f)
		if err != nil || total != len(ms) {
			t.Fatalf("list: %d %v", total, err)
		}
		var out []string
		for _, m := range ms {
			out = append(out, m.Title)
		}
		return out
	}
	if got := list(true); !slices.Equal(got, []string{"EN - Heat (1995)", "The Matrix (1999)"}) {
		t.Errorf("in library: %v", got)
	}
	if got := list(false); !slices.Equal(got, []string{"Alien (1979)", "Brazil (1985)", "Heat (1986)"}) {
		t.Errorf("not in library: %v", got)
	}
	in := true
	ss, total, err := e.vod.SeriesList(ctx, vod.Filter{InLibrary: &in, LibraryIDs: idx.IDs(true, all)})
	if err != nil || total != 1 || len(ss) != 1 || ss[0].ID != kings {
		t.Errorf("series in library: %+v %d %v", ss, total, err)
	}
}
