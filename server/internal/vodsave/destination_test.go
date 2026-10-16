package vodsave

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/library"
	"marquee/internal/settings"
	"marquee/internal/vod"
)

// withAnime adds save folders: Anime (films and shows, in no library yet) and Cartoons
// (shows only).
func withAnime(e *env) (anime, cartoons string) {
	e.t.Helper()
	anime, cartoons = filepath.Join(e.root, "media", "Anime"), filepath.Join(e.root, "media", "Cartoons")
	os.MkdirAll(anime, 0o755)
	os.MkdirAll(cartoons, 0o755)
	if _, err := e.s.Settings.Update(context.Background(), func(s *settings.Settings) error {
		s.VODSave.Destinations = []settings.VODSaveDestination{
			{ID: "anime", Name: "Anime", Folder: anime, Kind: "both"},
			{ID: "cartoons", Name: "Cartoons", Folder: cartoons, Kind: "shows"},
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return anime, cartoons
}

// A film saved to a save folder is named as usual, rooted there; the queue row says where
// it went, and warns while no library holds the folder (nothing is scanned); once a library
// does, that library is scanned.
func TestSaveFilmToDestination(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	anime, _ := withAnime(e)
	ctx := context.Background()
	film := e.movieID("EN - Clip Film (2020)")

	// The wrong kind, or one that isn't there, is refused.
	for _, dest := range []string{"cartoons", "nope"} {
		if _, err := e.s.SaveMovieTo(ctx, film, 1, false, dest); !errors.Is(err, ErrDestination) {
			t.Errorf("%s: %v", dest, err)
		}
	}
	if got := e.s.Destinations("movie"); len(got) != 1 || got[0].ID != "anime" {
		t.Errorf("film destinations: %+v", got)
	}
	if got := e.s.Destinations("episode"); len(got) != 2 {
		t.Errorf("episode destinations: %+v", got)
	}

	res, err := e.s.SaveMovieTo(ctx, film, 1, false, "anime")
	if err != nil || res.Queued != 1 {
		t.Fatalf("save: %+v %v", res, err)
	}
	v := res.Saves[0]
	if v.TargetPath != filepath.Join(anime, "Clip Film (2020)", "Clip Film (2020).mkv") || v.DestinationName != "Anime" || v.DestinationFolder != anime {
		t.Fatalf("queued: %+v", v)
	}
	list, _ := e.s.List(ctx)
	if len(list) != 1 || !list[0].NoLibrary || list[0].DestinationName != "Anime" {
		t.Fatalf("list: %+v", list)
	}
	e.start()
	done := e.wait(v.ID, Done, Failed)
	if done.State != Done || filepath.Dir(done.TargetPath) != filepath.Join(anime, "Clip Film (2020)") {
		t.Fatalf("done: %+v", done)
	}
	e.waitFor("the log", func() bool { return strings.Contains(e.logs.String(), "no library holds the folder") })
	e.scanMu.Lock()
	if len(e.scans) != 0 {
		t.Errorf("scanned %v with no library holding the folder", e.scans)
	}
	e.scanMu.Unlock()
	libs, _ := e.s.Libraries(ctx)
	if InLibrary(libs, anime) || !InLibrary(libs, e.movies) {
		t.Error("InLibrary")
	}

	// With an Anime library, saving there scans it.
	store := library.NewStore(e.s.DB)
	lib, err := store.Create(ctx, "Anime", library.Movies, []string{anime}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.s.ClearFinished(ctx)
	os.RemoveAll(filepath.Join(anime, "Clip Film (2020)"))
	res, err = e.s.SaveMovieTo(ctx, film, 1, false, "anime")
	if err != nil || res.Queued != 1 {
		t.Fatalf("again: %+v %v", res, err)
	}
	if list, _ := e.s.List(ctx); list[0].NoLibrary {
		t.Error("still no library")
	}
	e.wait(res.Saves[0].ID, Done)
	e.waitFor("the scan", func() bool { e.scanMu.Lock(); defer e.scanMu.Unlock(); return len(e.scans) == 1 })
	if e.scans[0] != lib.ID {
		t.Errorf("scanned %v, want the Anime library %d", e.scans, lib.ID)
	}
}

// A season saved to a save folder goes there, reusing the show's folder in it (not one in
// the default shows folder); one episode too; a films-only save folder is refused.
func TestSaveSeasonToDestination(t *testing.T) {
	e := newEnv(t, "[]", nil)
	_, cartoons := withAnime(e)
	if _, err := e.s.Settings.Update(context.Background(), func(s *settings.Settings) error {
		s.VODSave.Destinations = append(s.VODSave.Destinations, settings.VODSaveDestination{ID: "films", Name: "Films", Folder: e.movies, Kind: "movies"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.src.series = `[{"name":"EN - Kings: The Hill (2011)","series_id":"201","category_id":"1"}]`
	e.src.info["201"] = `{"seasons":[],"episodes":{"1":[
		{"id":"3001","episode_num":"1","title":"Winter","container_extension":"mkv","season":"1"},
		{"id":"3002","episode_num":"2","title":"Spring","container_extension":"mkv","season":"1"}],
		"2":[{"id":"3003","episode_num":"1","title":"Return","container_extension":"mkv","season":"2"}]}}`
	e.vod.Refresh(context.Background(), false)
	var seriesID int64
	e.s.DB.QueryRow("SELECT id FROM vod_series").Scan(&seriesID)
	ctx := context.Background()
	// The show has a folder in both; the save folder's is used, with its spelling.
	os.MkdirAll(filepath.Join(e.shows, "Kings - The Hill (2011)"), 0o755)
	os.MkdirAll(filepath.Join(cartoons, "KINGS - THE HILL (2011)"), 0o755)

	one := 1
	if _, err := e.s.SaveSeriesTo(ctx, seriesID, &one, 1, false, "films"); !errors.Is(err, ErrDestination) {
		t.Errorf("films-only folder: %v", err)
	}
	r, err := e.s.SaveSeriesTo(ctx, seriesID, &one, 1, false, "cartoons")
	if err != nil || r.Queued != 2 {
		t.Fatalf("season: %+v %v", r, err)
	}
	show := filepath.Join(cartoons, "KINGS - THE HILL (2011)", "Season 1")
	for i, want := range []string{"Kings - The Hill - S01E01 - Winter.mkv", "Kings - The Hill - S01E02 - Spring.mkv"} {
		if v := r.Saves[i]; v.TargetPath != filepath.Join(show, want) || v.DestinationName != "Cartoons" {
			t.Errorf("episode %d: %+v", i, v)
		}
	}
	var ep int64
	e.s.DB.QueryRow("SELECT id FROM vod_episodes WHERE source_key='3003'").Scan(&ep)
	if _, err := e.s.SaveEpisodeTo(ctx, ep, 1, false, "films"); !errors.Is(err, ErrDestination) {
		t.Errorf("episode to a films-only folder: %v", err)
	}
	r, err = e.s.SaveEpisodeTo(ctx, ep, 1, false, "anime")
	anime := filepath.Join(e.root, "media", "Anime")
	if err != nil || r.Queued != 1 || r.Saves[0].TargetPath != filepath.Join(anime, "Kings - The Hill (2011)", "Season 2", "Kings - The Hill - S02E01 - Return.mkv") {
		t.Fatalf("episode: %+v %v", r, err)
	}
}

// An upgrade replaces the library's copy where it is, whatever save folder was chosen; the
// library is checked the same as for the default folder (a copy as good is refused).
func TestUpgradeIgnoresDestination(t *testing.T) {
	films := `[{"name":"Heat (1995)","stream_id":"8","container_extension":"mkv"},{"name":"Brazil (1985)","stream_id":"11","container_extension":"mkv"}]`
	e := newEnv(t, films, nil)
	withAnime(e)
	ctx := context.Background()
	movies := e.libID("Movies")
	heat, brazil := e.movieID("Heat (1995)"), e.movieID("Brazil (1985)")
	e.libFile(movies, "movie", "Heat", 1995, 0, filepath.Join(e.movies, "Heat (1995)", "Heat.mkv"), 1280, 720, "h264", 4000)
	e.libFile(movies, "movie", "Brazil", 1985, 0, filepath.Join(e.movies, "Brazil (1985)", "Brazil.mkv"), 1920, 1080, "hevc", 8000)
	for _, id := range []int64{heat, brazil} {
		if err := e.vod.StoreMovieQuality(ctx, id, vod.Quality{Width: 1920, Height: 1080, VideoCodec: "h264", BitrateKbps: 6000, Origin: "info"}); err != nil {
			t.Fatal(err)
		}
	}
	var inLib *InLibraryError
	if _, err := e.s.SaveMovieTo(ctx, brazil, 1, false, "anime"); !errors.As(err, &inLib) {
		t.Errorf("as good in the library: %v", err)
	}
	if _, err := e.s.SaveMovieTo(ctx, heat, 1, false, "anime"); !errors.As(err, &inLib) || inLib.Match.Verdict != VerdictBetter {
		t.Errorf("better without upgrade: %v", err)
	}
	r, err := e.s.SaveMovieTo(ctx, heat, 1, true, "anime")
	if err != nil || r.Upgrades != 1 {
		t.Fatalf("upgrade: %+v %v", r, err)
	}
	if v := r.Saves[0]; filepath.Dir(v.TargetPath) != filepath.Join(e.movies, "Heat (1995)") || v.DestinationName != "" || v.ReplaceFileID == 0 {
		t.Errorf("upgrade went to %+v", v)
	}
}

func TestDestinationSettingsValidation(t *testing.T) {
	base := settings.Defaults().VODSave
	ok := settings.VODSaveDestination{ID: "a", Name: "Anime", Folder: "/media/Anime", Kind: "both"}
	for name, ds := range map[string][]settings.VODSaveDestination{
		"no name":   {{ID: "a", Folder: "/x", Kind: "both"}},
		"same name": {ok, {ID: "b", Name: " anime ", Folder: "/y", Kind: "shows"}},
		"same id":   {ok, {ID: "a", Name: "Other", Folder: "/y", Kind: "shows"}},
		"relative":  {{ID: "a", Name: "A", Folder: "media/Anime", Kind: "both"}},
		"no folder": {{ID: "a", Name: "A", Kind: "both"}},
		"kind":      {{ID: "a", Name: "A", Folder: "/x", Kind: "music"}},
		"no id":     {{Name: "A", Folder: "/x", Kind: "both"}},
	} {
		s := settings.Defaults()
		s.VODSave = base
		s.VODSave.Destinations = ds
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := settings.Defaults()
	s.VODSave.Destinations = []settings.VODSaveDestination{ok, {ID: "b", Name: "Cartoons", Folder: "/y", Kind: "shows"}}
	if err := s.Validate(); err != nil {
		t.Errorf("valid: %v", err)
	}
}
