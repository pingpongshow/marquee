package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"marquee/internal/api"
	"marquee/internal/soundprint"
)

// Stations and mixes say what they are (the apps send it back with plays, for the playlist
// statistics); mood radios and steered radios answer; the sound model setting starts a trial
// that can be kept or (once done) switched to (MUSIC-1, MUSIC-17, MUSIC-18).
func TestMusicLearningAPI(t *testing.T) {
	h := newHarness(t)
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		out := make([][]float32, len(req.Texts))
		for i := range out {
			out[i] = []float32{1, 0, 0}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "clap", "embeddings": out})
	}))
	defer sidecar.Close()
	sp := &soundprint.Service{DB: h.db, Index: soundprint.NewIndex(), Client: &soundprint.Client{BaseURL: sidecar.URL},
		Model: func() string { return h.handlers.Settings.Get().Music.SoundModel }, Enabled: func() bool { return true }}
	h.handlers.Soundprint = sp
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &lib)
	var tracks []int64
	for a := 0; a < 12; a++ {
		r, _ := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', ?, ?)`, lib.Id, fmt.Sprint("Artist ", a), "a")
		artist, _ := r.LastInsertId()
		r, _ = h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id) VALUES (?, 'album', 'Album', 'album', ?)`, lib.Id, artist)
		album, _ := r.LastInsertId()
		for n := 0; n < 5; n++ {
			title := fmt.Sprint("Song ", a, "-", n)
			r, _ = h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, duration_ms) VALUES (?, 'track', ?, ?, ?, ?, 200000)`,
				lib.Id, title, title, album, artist)
			id, _ := r.LastInsertId()
			sp.Index.Put(&soundprint.Track{ItemID: id, AlbumID: album, ArtistID: artist, LibraryID: lib.Id, DurationMS: 200000,
				Vec: []float32{1, float32(a) / 10, float32(n) / 100}, Copy: soundprint.CopyKey(title, artist), Energy: float64(n) / 5})
			tracks = append(tracks, id)
		}
	}
	// Liked enough for Daily Mixes.
	for _, id := range tracks[:20] {
		h.db.Exec(`INSERT INTO user_item_state(user_id, item_id, play_count) VALUES (?, ?, 3)`, res.User.Id, id)
	}

	var st api.Station
	if code := h.do("POST", "/music/radio", map[string]any{"seed": "mood", "value": "chill", "limit": 10}, &st); code != 200 {
		t.Fatalf("mood radio: %d", code)
	}
	if st.Title != "Chill Radio" || st.Context == nil || *st.Context != "radio:mood" || len(st.Items) != 10 {
		t.Errorf("mood radio: %q %v %d", st.Title, st.Context, len(st.Items))
	}
	if code := h.do("POST", "/music/radio", map[string]any{"seed": "item", "itemId": tracks[0], "limit": 10,
		"exclude": tracks[:3], "skipped": tracks[1:3], "finished": tracks[:1]}, &st); code != 200 || st.Context == nil || *st.Context != "radio:item" {
		t.Errorf("steered radio: %d %v", code, st.Context)
	}
	var mixes []api.Station
	h.do("GET", fmt.Sprintf("/music/mixes?libraryId=%d", lib.Id), nil, &mixes)
	if len(mixes) == 0 || mixes[0].Context == nil || *mixes[0].Context != "mix:daily" {
		t.Fatalf("mixes: %+v", mixes)
	}

	// The sound model: choosing the music model starts a trial; switching waits for it;
	// keeping sets the setting back.
	var set api.ServerSettings
	h.do("PATCH", "/settings", map[string]any{"music": map[string]any{"soundModel": "music", "communityData": false}}, &set)
	if set.Music == nil || *set.Music.SoundModel != "music" || *set.Music.CommunityData {
		t.Fatalf("settings: %+v", set.Music)
	}
	var status api.MusicStatus
	h.do("GET", "/music/status", nil, &status)
	if status.SoundModel == nil || *status.SoundModel != "general" || status.Trial == nil || status.Trial.Model != "music" || status.Trial.Tried != nil {
		t.Errorf("status: %+v %+v", status.SoundModel, status.Trial)
	}
	if code := h.do("POST", "/music/sound-model", map[string]any{"action": "switch"}, nil); code != 409 {
		t.Errorf("switch before the trial is done: %d", code)
	}
	if code := h.do("POST", "/music/sound-model", map[string]any{"action": "keep"}, nil); code != 204 {
		t.Errorf("keep: %d", code)
	}
	h.do("GET", "/settings", nil, &set)
	if *set.Music.SoundModel != "general" {
		t.Errorf("kept, but the setting is %q", *set.Music.SoundModel)
	}
	var after api.MusicStatus
	h.do("GET", "/music/status", nil, &after)
	if after.Trial != nil {
		t.Errorf("a trial after keeping: %+v", after.Trial)
	}
}
