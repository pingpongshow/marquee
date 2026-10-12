package soundprint

import (
	"context"
	"sort"
	"testing"
)

func sorted(ids []int64) []int64 {
	out := append([]int64(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func only(ids []int64, ok func(int64) bool) bool {
	for _, id := range ids {
		if !ok(id) {
			return false
		}
	}
	return len(ids) > 0
}

// Tempo and energy are hard filters with tolerance; hits and deep cuts use music_popular;
// country, area, type and gender come from music_info; release types from albums.
func TestMuseQueryDetailFilters(t *testing.T) {
	s, d := museLibrary(t)
	ctx := context.Background()
	for i := int64(1); i <= 20; i++ {
		tr := s.Index.Get(i)
		tr.AlbumID = map[bool]int64{true: 2001, false: 2002}[i <= 10]
		tr.BPM = 60 + float64(i)*5 // 65 … 160
		tr.Energy = float64(i) / 20
		tr.Mode = map[bool]string{true: "minor", false: "major"}[i%2 == 0]
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// The hard filters' result (MuseQuery then picks from it by sound).
	run := func(q MusicQuery) []int64 {
		t.Helper()
		f := s.museFilters(ctx, 1, q)
		return TrackIDs(s.Index.Where(q.Keep(f, nil, true)))
	}
	// Fast (120+, 5% tolerance: 114 and up) → tracks 11–20 (115 BPM and up).
	if ids := run(MusicQuery{TempoMin: 120}); !only(ids, func(id int64) bool { return id >= 11 }) || len(ids) != 10 {
		t.Errorf("fast: %v", sorted(ids))
	}
	// Around 120 (112–128, 5% tolerance): 110–130 BPM, and 65 BPM counted double (130).
	if ids := sorted(run(MusicQuery{TempoMin: 112, TempoMax: 128})); len(ids) != 6 || ids[0] != 1 || ids[1] != 10 || ids[5] != 14 {
		t.Errorf("around 120: %v", ids)
	}
	// High energy is the top part of the library, low the bottom.
	if ids := run(MusicQuery{Energy: "high"}); !only(ids, func(id int64) bool { return id >= 10 }) {
		t.Errorf("high energy: %v", sorted(ids))
	}
	if ids := run(MusicQuery{Energy: "low"}); !only(ids, func(id int64) bool { return id <= 10 }) {
		t.Errorf("low energy: %v", sorted(ids))
	}
	// Hits and deep cuts.
	exec(`INSERT INTO music_popular(artist_id, track_id, rank) VALUES (1001, 4, 1), (1001, 5, 2), (1001, 6, 9)`)
	if ids := sorted(run(MusicQuery{Popular: 1})); len(ids) != 2 || ids[0] != 4 || ids[1] != 5 {
		t.Errorf("hits: %v", ids)
	}
	if ids := run(MusicQuery{Popular: -1}); len(ids) != 17 {
		t.Errorf("deep cuts: %v", sorted(ids))
	}
	// Artist details: the Rockers are a British band from Manchester, Jazz Cats a Swedish woman.
	exec(`INSERT INTO music_info(item_id, kind, found, version, checked_at, artist_type, gender, country, area, begin_area, begin_year, end_year)
		VALUES (1001, 'artist', 1, 2, 'x', 'group', NULL, 'GB', 'United Kingdom', 'Manchester', 1985, 1995),
		       (1002, 'artist', 1, 2, 'x', 'person', 'female', 'SE', 'Sweden', 'Stockholm', 1958, NULL)`)
	jazz := func(id int64) bool { return id > 10 }
	rock := func(id int64) bool { return id <= 10 }
	for name, c := range map[string]struct {
		q  MusicQuery
		ok func(int64) bool
	}{
		"country":      {MusicQuery{Countries: []string{"SE"}}, jazz},
		"area":         {MusicQuery{Areas: []string{"manchester"}}, rock},
		"bands":        {MusicQuery{ArtistType: "group"}, rock},
		"female":       {MusicQuery{Gender: "female"}, jazz},
		"active 2000s": {MusicQuery{ActiveFrom: 2000, ActiveTo: 2009}, jazz},
	} {
		if ids := run(c.q); !only(ids, c.ok) || len(ids) != 10 {
			t.Errorf("%s: %v", name, sorted(ids))
		}
	}
	// Release types: the Rockers' album is live, the Jazz Cats' a compilation.
	exec(`UPDATE items SET release_type = 'live' WHERE id = 2001`)
	exec(`UPDATE items SET release_type = 'compilation' WHERE id = 2002`)
	if ids := run(MusicQuery{AlbumTypes: []string{"live"}}); !only(ids, rock) || len(ids) != 10 {
		t.Errorf("live: %v", sorted(ids))
	}
	if ids := run(MusicQuery{ExcludeAlbumTypes: []string{"compilation"}}); !only(ids, rock) || len(ids) != 10 {
		t.Errorf("no compilations: %v", sorted(ids))
	}
	// Mode is a soft boost (and key with it), never a filter.
	b := MusicQuery{Mode: "minor", Key: "D"}.keyBoosts(s.Index.Where(nil))
	if b[2] != modeBoost || b[1] != 0 {
		t.Errorf("mode boost %v", b)
	}
	s.Index.Get(2).Key = "D"
	if b := (MusicQuery{Mode: "minor", Key: "D"}).keyBoosts(s.Index.Where(nil)); b[2] != modeBoost+keyBoost {
		t.Errorf("key boost %v", b)
	}
	// Through MuseQuery: filters and boosts together still make a playlist.
	st, err := s.MuseQuery(ctx, 1, MusicQuery{Mode: "minor", Genres: []string{"Rock"}, TempoMin: 80}, 5, Options{})
	if err != nil || !only(st.IDs, func(id int64) bool { return id >= 4 && id <= 10 }) {
		t.Errorf("minor fast rock: %v %v", st.IDs, err)
	}
}

// Descriptive criteria: the best-matching artists filter the tracks; when they have too few
// tracks for the playlist they become a boost.
func TestMuseQueryDescribedArtists(t *testing.T) {
	s, _ := museLibrary(t)
	ctx := context.Background()
	q := MusicQuery{Describe: "cool jazz", ArtistScores: map[int64]float64{1001: 0.2, 1002: 0.8}}
	f := s.museFilters(ctx, 1, q)
	f.Described = q.DescribedSet()
	if ids := TrackIDs(s.Index.Where(q.Keep(f, nil, true))); !only(ids, func(id int64) bool { return id > 10 }) || len(ids) != 10 {
		t.Fatalf("described filter: %v", ids)
	}
	st, err := s.MuseQuery(ctx, 1, q, 5, Options{})
	if err != nil || !only(st.IDs, func(id int64) bool { return id > 10 }) {
		t.Fatalf("described playlist: %v %v", st.IDs, err)
	}
	// Twenty wanted, ten by the matching artist: the filter becomes a boost, so the
	// matching artist's tracks lead.
	st, _ = s.MuseQuery(ctx, 1, q, 20, Options{})
	if len(st.IDs) == 0 || st.IDs[0] <= 10 {
		t.Fatalf("relaxed: %v", st.IDs)
	}
	b := q.describedBoost(s.Index.Where(nil))
	if b[11] != describeBoost || b[1] != 0 {
		t.Fatalf("boost %v", b)
	}
	if set := q.DescribedSet(); !set[1002] || set[1001] {
		t.Fatalf("described set %v", set)
	}
}

func TestEnergySpreadAndTempo(t *testing.T) {
	var ts []*Track
	for i := 1; i <= 11; i++ {
		ts = append(ts, &Track{Energy: float64(i) / 10})
	}
	ts = append(ts, &Track{}) // not measured
	low, high := EnergySpread(ts)
	if low != 0.5 || high != 0.6 {
		t.Errorf("spread %v %v", low, high)
	}
	q := MusicQuery{TempoMin: 115, TempoMax: 125}
	for bpm, want := range map[float64]bool{120: true, 110: true, 60: true, 240: true, 90: false, 0: false} {
		if q.tempoOK(bpm) != want {
			t.Errorf("tempo %v: %v", bpm, !want)
		}
	}
	if (MusicQuery{TempoMax: 95}).tempoOK(170) {
		t.Error("slow shouldn't accept half time")
	}
}
