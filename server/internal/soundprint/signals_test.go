package soundprint

import (
	"context"
	"testing"
	"time"
)

const day = 24 * time.Hour

func TestNegatives(t *testing.T) {
	skip := func(id, artist int64, age time.Duration) PlayEvent {
		return PlayEvent{ItemID: id, ArtistID: artist, Age: age, PositionMS: 8000, DurationMS: 240000}
	}
	full := func(id, artist int64, age time.Duration) PlayEvent {
		return PlayEvent{ItemID: id, ArtistID: artist, Age: age, PositionMS: 235000, DurationMS: 240000}
	}
	events := []PlayEvent{
		skip(1, 10, 0),      // skipped today: full weight
		skip(2, 10, 15*day), // half faded
		skip(3, 10, 40*day), // too old: ignored
		skip(4, 20, 0),      // skipped, but played in full since
		full(4, 20, day/2),  //
		skip(5, 20, 0),      // skipped, but rated 4 stars
		{ItemID: 6, ArtistID: 20, PositionMS: 100000, DurationMS: 240000}, // stopped half way: neither
	}
	got := Negatives(events, map[int64]float64{5: 8, 7: 2}, nil)
	if w := got[1]; w < 0.39 || w > 0.41 {
		t.Errorf("fresh skip %v, want 0.4", w)
	}
	if w := got[2]; w < 0.19 || w > 0.21 {
		t.Errorf("half-faded skip %v, want 0.2", w)
	}
	for _, id := range []int64{3, 4, 5, 6} {
		if _, ok := got[id]; ok {
			t.Errorf("track %d avoided (%v)", id, got[id])
		}
	}
	if got[7] != 1 {
		t.Errorf("2-star track %v, want 1", got[7])
	}
	// Repeated skips add up, capped at 1.
	many := Negatives([]PlayEvent{skip(8, 0, 0), skip(8, 0, 0), skip(8, 0, 0)}, nil, nil)
	if many[8] != 1 {
		t.Errorf("three skips %v, want 1", many[8])
	}
}

func TestNegativesArtist(t *testing.T) {
	var events []PlayEvent
	for id := int64(1); id <= 4; id++ {
		events = append(events, PlayEvent{ItemID: id, ArtistID: 9, PositionMS: 5000, DurationMS: 200000})
	}
	artistTracks := map[int64][]int64{9: {1, 2, 3, 4, 5, 6}}
	got := Negatives(events, map[int64]float64{6: 9}, artistTracks)
	// Four skips, two allowed: the artist's other tracks get 0.2; the 4.5-star one none.
	if w := got[5]; w < 0.19 || w > 0.21 {
		t.Errorf("unplayed track by a skipped artist %v, want 0.2", w)
	}
	if _, ok := got[6]; ok {
		t.Error("a highly rated track was avoided")
	}
	if got[1] < 0.39 {
		t.Errorf("skipped track %v", got[1])
	}
	// Two skips are within the allowance.
	got = Negatives(events[:2], nil, artistTracks)
	if _, ok := got[5]; ok {
		t.Error("two skips penalised the artist")
	}
}

func TestDecay(t *testing.T) {
	if decay(0) != 1 || decay(30*day) != 0 || decay(-day) != 1 {
		t.Fatal("decay bounds")
	}
	if d := decay(15 * day); d < 0.49 || d > 0.51 {
		t.Fatalf("decay(15d) = %v", d)
	}
}

func TestAvoidLearnsFromHistory(t *testing.T) {
	s, d := museLibrary(t)
	now := time.Now().UTC()
	ts := func(ago time.Duration) string { return now.Add(-ago).Format("2006-01-02T15:04:05.000Z") }
	// Track 2 skipped today, track 3 skipped 50 days ago, track 4 rated 1 star.
	d.Exec(`INSERT INTO play_history (user_id, item_id, item_title, started_at, stopped_at, position_ms) VALUES (1, 2, 't', ?, ?, 4000)`, ts(time.Hour), ts(time.Hour))
	d.Exec(`INSERT INTO play_history (user_id, item_id, item_title, started_at, stopped_at, position_ms) VALUES (1, 3, 't', ?, ?, 4000)`, ts(50*day), ts(50*day))
	d.Exec(`INSERT INTO user_item_state (user_id, item_id, rating) VALUES (1, 4, 2)`)
	on := true
	s.Learn = func() bool { return on }
	got := s.Avoid(context.Background(), 1)
	if w := got[2]; w < 0.39 || w > 0.41 {
		t.Errorf("today's skip %v, want 0.4", w)
	}
	if _, ok := got[3]; ok {
		t.Error("a skip from 50 days ago still counts")
	}
	if got[4] != 1 {
		t.Errorf("1-star track %v", got[4])
	}
	// Learning off: the older rules (60 days of skips) apply.
	on = false
	if old := s.Avoid(context.Background(), 1); old[3] == 0 {
		t.Error("with learning off the old 60-day skip rule should apply")
	}
}
