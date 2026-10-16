package vod

import (
	"context"
	"testing"
)

// Films and episodes stopped part-way are in Continue Watching (one episode per series),
// newest first; removing one hides it until it's watched again; watched ones and ones
// rated above the profile's limit aren't there.
func TestContinueWatchingAll(t *testing.T) {
	s, _, _, _, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	kings := seriesByTitle(t, s, uid)["Kings of the Hill"]
	s.LoadEpisodes(ctx, kings.ID, false)
	eps, _ := s.Episodes(ctx, kings.ID, uid)
	var film int64
	s.DB.QueryRow("SELECT id FROM vod_movies ORDER BY id LIMIT 1").Scan(&film)
	at := func(table, col string, id int64, when string) {
		if _, err := s.DB.Exec("UPDATE "+table+" SET updated_at=? WHERE user_id=? AND "+col+"=?", when, uid, id); err != nil {
			t.Fatal(err)
		}
	}
	s.SetEpisodeProgress(ctx, uid, eps[0].ID, 600_000, 3_000_000)
	at("vod_episode_progress", "episode_id", eps[0].ID, "2026-01-01T10:00:00Z")
	s.SetEpisodeProgress(ctx, uid, eps[1].ID, 700_000, 3_000_000)
	at("vod_episode_progress", "episode_id", eps[1].ID, "2026-01-03T10:00:00Z")
	s.SetProgress(ctx, uid, film, 900_000, 5_000_000)
	at("vod_progress", "movie_id", film, "2026-01-02T10:00:00Z")

	list := func(maxRating string) []InProgress {
		t.Helper()
		l, err := s.ContinueWatchingAll(ctx, uid, maxRating)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := list("")
	if len(l) != 2 || l[0].Episode == nil || l[0].Episode.ID != eps[1].ID || l[0].PositionMS != 700_000 ||
		l[1].Movie == nil || l[1].Movie.ID != film || l[1].PositionMS != 900_000 {
		t.Fatalf("list: %+v", l)
	}
	// A film rated above the limit (unrated counts) is left out.
	if l := list("PG"); len(l) != 0 {
		t.Errorf("restricted: %+v", l)
	}
	// Removing hides until watched again.
	if err := s.HideMovieFromContinue(ctx, uid, film); err != nil {
		t.Fatal(err)
	}
	if err := s.HideEpisodeFromContinue(ctx, uid, eps[1].ID); err != nil {
		t.Fatal(err)
	}
	if l := list(""); len(l) != 0 {
		t.Fatalf("hidden: %+v", l)
	}
	at("vod_progress", "movie_id", film, "2099-01-01T00:00:00Z")
	if l := list(""); len(l) != 1 || l[0].Movie == nil {
		t.Fatalf("watched again: %+v", l)
	}
	if err := s.HideMovieFromContinue(ctx, uid, 99999); err != ErrNotFound {
		t.Errorf("missing film: %v", err)
	}
	// Finished: gone.
	s.SetWatched(ctx, uid, film, true)
	if l := list(""); len(l) != 0 {
		t.Errorf("watched: %+v", l)
	}
}

func TestQualityFromInfo(t *testing.T) {
	q, ok := qualityFromInfo([]byte(`{"video":{"codec_name":"hevc","width":"3840","height":2160,"color_transfer":"smpte2084"},"bitrate":"15000"}`))
	if !ok || q.Width != 3840 || q.Height != 2160 || q.VideoCodec != "hevc" || q.HDR != "HDR10" || q.BitrateKbps != 15000 || q.Origin != "info" {
		t.Errorf("hdr: %+v %v", q, ok)
	}
	q, ok = qualityFromInfo([]byte(`{"video":{"codec_name":"h264","width":1920,"height":1080,"side_data_list":[{"side_data_type":"DOVI configuration record"}]}}`))
	if !ok || q.HDR != "DV" || q.BitrateKbps != 0 {
		t.Errorf("dv: %+v", q)
	}
	for _, in := range []string{`{"video":[]}`, `{}`, `[]`, `{"video":{"codec_name":"h264"}}`, ``} {
		if _, ok := qualityFromInfo([]byte(in)); ok {
			t.Errorf("%q: known", in)
		}
	}
}
