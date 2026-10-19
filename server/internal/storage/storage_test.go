package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
)

func open(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, q := range []string{
		`INSERT INTO libraries(id, name, type, sort_order) VALUES (1, 'Movies', 'movies', 0), (2, 'TV', 'shows', 1), (3, 'Recorded TV', 'shows', 2)`,
		`UPDATE libraries SET recordings = 1 WHERE id = 3`,
		// Heat (two parts and an extra), Alien (gone), a show of three episodes, one gone.
		`INSERT INTO items(id, library_id, type, parent_id, grandparent_id, title, sort_title, year, extra_type) VALUES
			(1, 1, 'movie', NULL, NULL, 'Heat', 'Heat', 1995, NULL),
			(2, 1, 'video', 1, NULL, 'Making Of', 'Making Of', NULL, 'featurette'),
			(3, 1, 'movie', NULL, NULL, 'Alien', 'Alien', 1979, NULL),
			(10, 2, 'show', NULL, NULL, 'Lost', 'Lost', 2004, NULL),
			(11, 2, 'season', 10, NULL, 'Season 1', 'Season 1', NULL, NULL),
			(12, 2, 'episode', 11, 10, 'Pilot', 'Pilot', NULL, NULL),
			(13, 2, 'episode', 11, 10, 'Tabula Rasa', 'Tabula Rasa', NULL, NULL),
			(14, 2, 'episode', 11, 10, 'Walkabout', 'Walkabout', NULL, NULL)`,
		`INSERT INTO media_versions(id, item_id) VALUES (1, 1), (2, 2), (3, 3), (12, 12), (13, 13), (14, 14)`,
		`INSERT INTO media_files(version_id, library_id, path, size, mtime, available) VALUES
			(1, 1, '/m/heat1.mkv', 4000, 0, 1), (1, 1, '/m/heat2.mkv', 3000, 0, 1), (2, 1, '/m/heat-extra.mkv', 500, 0, 1),
			(3, 1, '/m/alien.mkv', 99999, 0, 0),
			(12, 2, '/t/1.mkv', 2000, 0, 1), (13, 2, '/t/2.mkv', 2500, 0, 1), (14, 2, '/t/3.mkv', 88888, 0, 0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return &Service{DB: d}
}

// Library sizes count available files only; recordings libraries are listed too.
func TestLibraries(t *testing.T) {
	s := open(t)
	libs, err := s.Libraries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 3 {
		t.Fatalf("libraries: %+v", libs)
	}
	if l := libs[0]; l.Name != "Movies" || l.Bytes != 7500 || l.Files != 3 || l.Recordings {
		t.Errorf("movies: %+v", l)
	}
	if l := libs[1]; l.Name != "TV" || l.Bytes != 4500 || l.Files != 2 {
		t.Errorf("tv: %+v", l)
	}
	if l := libs[2]; l.Bytes != 0 || l.Files != 0 || !l.Recordings {
		t.Errorf("recordings: %+v", l)
	}
}

// A show sums its episodes, a movie its parts and extras; missing files don't count; top N.
func TestLargest(t *testing.T) {
	s := open(t)
	got, err := s.Largest(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("largest: %+v", got)
	}
	if g := got[0]; g.ItemID != 1 || g.Type != "movie" || g.Bytes != 7500 || g.Files != 3 || g.Year != 1995 {
		t.Errorf("first: %+v", g)
	}
	if g := got[1]; g.ItemID != 10 || g.Type != "show" || g.Bytes != 4500 || g.Files != 2 || g.LibraryID != 2 {
		t.Errorf("second: %+v", g)
	}
	if got, _ := s.Largest(context.Background(), 1); len(got) != 1 || got[0].ItemID != 1 {
		t.Errorf("top 1: %+v", got)
	}
}

// One sample a day; samples older than a year go; History returns the asked-for days.
func TestSampleOncePerDay(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local)
	s.Now = func() time.Time { return now }
	// An old sample to prune, and one 40 days ago.
	if _, err := s.DB.Exec(`INSERT INTO storage_samples(library_id, date, bytes, files) VALUES (1, '2025-01-01', 1, 1), (1, '2026-09-08', 5000, 2)`); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Sample(ctx); err != nil || !ok {
		t.Fatalf("first sample: %v %v", ok, err)
	}
	// Files added later the same day don't make another sample.
	s.DB.Exec(`UPDATE media_files SET size = size * 2`)
	now = now.Add(5 * time.Hour)
	if ok, err := s.Sample(ctx); err != nil || ok {
		t.Fatalf("second sample the same day: %v %v", ok, err)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM storage_samples WHERE date = '2026-10-18'`).Scan(&n)
	if n != 3 {
		t.Errorf("today's samples = %d, want one per library", n)
	}
	s.DB.QueryRow(`SELECT COUNT(*) FROM storage_samples WHERE date = '2025-01-01'`).Scan(&n)
	if n != 0 {
		t.Error("a sample over a year old wasn't pruned")
	}
	now = now.AddDate(0, 0, 1)
	if ok, _ := s.Sample(ctx); !ok {
		t.Error("no sample the next day")
	}

	h, err := s.History(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := h[1]; len(got) != 2 || got[0].Date != "2026-10-18" || got[0].Bytes != 7500 || got[1].Bytes != 15000 {
		t.Errorf("30 days: %+v", got)
	}
	if h, _ = s.History(ctx, 90); len(h[1]) != 3 || h[1][0].Date != "2026-09-08" {
		t.Errorf("90 days: %+v", h[1])
	}
}

// Folders on one file system share an entry labelled with all of them; unreadable ones go.
func TestVolumes(t *testing.T) {
	s := &Service{
		Folders: func(context.Context) []Folder {
			return []Folder{{"Config", "/config"}, {"Transcode", "/config/transcode"}, {"Recordings", "/gone"},
				{"Movies", "/media/movies"}, {"Movies", "/media/movies2"}, {"TV", "/media/tv"}}
		},
		Device: func(p string) string {
			switch p {
			case "/config", "/config/transcode":
				return "a"
			case "/gone":
				return "c"
			}
			return "b"
		},
		Usage: func(p string) (int64, int64, error) {
			if p == "/gone" {
				return 0, 0, errors.New("no such folder")
			}
			if p == "/config" {
				return 100, 50, nil
			}
			return 1000, 30, nil
		},
	}
	got := s.Volumes(context.Background())
	if len(got) != 2 {
		t.Fatalf("volumes: %+v", got)
	}
	if v := got[0]; v.Path != "/config" || v.Label != "Config, Transcode" || v.TotalBytes != 100 || v.FreeBytes != 50 {
		t.Errorf("config volume: %+v", v)
	}
	if v := got[1]; v.Path != "/media/movies" || v.Label != "Movies, TV" || v.FreeBytes != 30 {
		t.Errorf("media volume: %+v", v)
	}
}
