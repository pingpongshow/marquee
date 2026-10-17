package metadata

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/metadata/deezer"
	"marquee/internal/metadata/musicbrainz"
)

const (
	twinA  = "aaaaaaaa-0000-0000-0000-000000000001" // the wrong Twins
	twinB  = "bbbbbbbb-0000-0000-0000-000000000002"
	rgLive = "cccccccc-0000-0000-0000-000000000003"
	relOne = "dddddddd-0000-0000-0000-000000000004" // a release of rgLive
)

func TestParseMBRef(t *testing.T) {
	for q, want := range map[string][2]string{
		twinB:                              {"", twinB},
		" " + strings.ToUpper(twinB) + " ": {"", twinB},
		"https://musicbrainz.org/artist/" + twinB:          {"artist", twinB},
		"musicbrainz.org/release-group/" + rgLive + "?x=1": {"release-group", rgLive},
		"https://beta.musicbrainz.org/release/" + relOne:   {"release", relOne},
	} {
		kind, id, ok := parseMBRef(q)
		if !ok || kind != want[0] || id != want[1] {
			t.Errorf("%q → %s %s %v", q, kind, id, ok)
		}
	}
	for _, q := range []string{"Twins", "1979", "Twins " + twinB, ""} {
		if _, _, ok := parseMBRef(q); ok {
			t.Errorf("%q should be a search", q)
		}
	}
}

func TestMusicMatch(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		q := r.URL.Query().Get("query")
		switch {
		case r.URL.Path == "/artist" && strings.Contains(q, `"Twins"`):
			io.WriteString(w, `{"artists":[{"id":"`+twinA+`","name":"Twins","score":100,"type":"Group","country":"DE","life-span":{"begin":"1980","end":"1987"}},
				{"id":"`+twinB+`","name":"Twins","score":100,"type":"Person","disambiguation":"US singer","area":{"name":"US","iso-3166-1-codes":["US"]}}]}`)
		case r.URL.Path == "/artist/"+twinB:
			io.WriteString(w, `{"id":"`+twinB+`","name":"Twins","sort-name":"Twins","type":"Person","gender":"Female","country":"US","disambiguation":"US singer",
				"life-span":{"begin":"1990"},"genres":[{"name":"soul","count":4}],"tags":[{"name":"neo soul","count":2}],
				"relations":[{"type":"wikidata","url":{"resource":"https://www.wikidata.org/wiki/Q2"}},{"type":"free streaming","url":{"resource":"https://www.deezer.com/artist/42"}}]}`)
		case r.URL.Path == "/wiki/Special:EntityData/Q2.json":
			io.WriteString(w, `{"entities":{"Q2":{"sitelinks":{"enwiki":{"title":"Twins (singer)"}}}}}`)
		case r.URL.Path == "/en/api/rest_v1/page/summary/Twins_(singer)":
			io.WriteString(w, `{"type":"standard","extract":"Twins is an American soul singer."}`)
		case r.URL.Path == "/1/popularity/top-recordings-for-artist/"+twinB:
			io.WriteString(w, `[{"recording_mbid":"r1","recording_name":"Right Song","total_listen_count":5}]`)
		case r.URL.Path == "/dz/artist/42":
			io.WriteString(w, `{"name":"Twins","picture_xl":"https://cdn.example/right.jpg"}`)
		case r.URL.Path == "/release-group" && strings.Contains(q, `"Live"`):
			if !strings.Contains(q, `artist:"Twins"`) {
				t.Errorf("album search without its artist: %s", q)
			}
			io.WriteString(w, `{"release-groups":[{"id":"`+rgLive+`","title":"Live","score":98,"primary-type":"Album","secondary-types":["Live"],
				"first-release-date":"1994-03-01","artist-credit":[{"name":"Twins","joinphrase":" & ","artist":{"id":"`+twinB+`"}},{"name":"Friends"}]}]}`)
		case r.URL.Path == "/release-group/"+rgLive:
			io.WriteString(w, `{"id":"`+rgLive+`","title":"Live","primary-type":"Album","secondary-types":["Live"],"first-release-date":"1994-03-01",
				"genres":[{"name":"soul","count":1}],"tags":[{"name":"live soul","count":3}]}`)
		case r.URL.Path == "/release/"+relOne:
			io.WriteString(w, `{"id":"`+relOne+`","release-group":{"id":"`+rgLive+`"},"label-info":[{"label":{"name":"Motown"}}]}`)
		case r.URL.Path == "/release" && r.URL.Query().Get("release-group") == rgLive:
			io.WriteString(w, `{"releases":[{"date":"2001","label-info":[{"label":{"name":"Reissue Co"}}]},{"date":"1994-03-01","label-info":[{"label":{"name":"Motown"}}]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	mb := musicbrainz.New("test")
	mb.BaseURL, mb.Interval = srv.URL, time.Millisecond
	dz := deezer.New()
	dz.BaseURL = srv.URL + "/dz"
	s := &Service{DB: d, mb: mb, deezer: dz, web: &musicbrainz.Web{Wikidata: srv.URL, Wikipedia: srv.URL + "/%s", ListenBrainz: srv.URL, UserAgent: "test", HTTP: srv.Client()}}

	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Music', 'music'), (2, 'Movies', 'movies')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, scan_key, summary) VALUES (1, 1, 'artist', 'Twins', 'Twins', 'a:twins', 'A German band.')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, scan_key, parent_id, year, originally_available_at, release_type)
		VALUES (2, 1, 'album', 'Live', 'Live', 'al:twins:live', 1, 2001, '2001-06-01', 'album')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (3, 1, 'track', 'Right Song', '1', 2, 1), (4, 1, 'track', 'Wrong Song', '2', 2, 1)`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, scan_key, summary, locked_fields) VALUES (5, 1, 'artist', 'Mine', 'Mine', 'a:mine', 'Written by hand.', '["summary","genres"]')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title) VALUES (6, 2, 'movie', 'Heat', 'Heat')`)
	mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (1, 'musicbrainz', '`+twinA+`'), (2, 'musicbrainz', 'tagged-release'), (5, 'musicbrainz', '`+twinA+`')`)
	mustExec(t, d, `INSERT INTO music_info(item_id, kind, found, version, checked_at, artist_type, country, begin_year, end_year) VALUES (1, 'artist', 1, 2, 'x', 'group', 'DE', 1980, 1987)`)
	mustExec(t, d, `INSERT INTO musicbrainz_lookups(item_id, checked_at, found) VALUES (1, 'x', 1), (2, 'x', 1)`)
	mustExec(t, d, `INSERT INTO tags(id, kind, name) VALUES (1, 'style', 'krautrock'), (2, 'genre', 'Krautrock'), (3, 'genre', 'Live Rock')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (1, 1), (1, 2), (5, 2), (2, 3)`)
	mustExec(t, d, `INSERT INTO music_popular(artist_id, track_id, rank) VALUES (1, 4, 1)`)
	mustExec(t, d, `INSERT INTO artwork(item_id, kind, source, remote_url, selected) VALUES (1, 'poster', 'deezer', 'https://cdn.example/wrong.jpg', 1)`)
	str := func(q string, args ...any) string {
		var v string
		d.QueryRow(q, args...).Scan(&v)
		return v
	}
	tags := func(id int, kind string) string {
		return str(`SELECT COALESCE(group_concat(name, ','), '') FROM (SELECT t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id WHERE it.item_id = ? AND t.kind = ? ORDER BY t.name)`, id, kind)
	}

	// Search by the artist's name; the current match is marked.
	cands, err := s.MusicCandidates(ctx, 1, "", "")
	if err != nil || len(cands) != 2 {
		t.Fatalf("candidates: %v %+v", err, cands)
	}
	if c := cands[0]; c.MBID != twinA || !c.Current || c.Type != "group" || c.Country != "DE" || c.BeginYear != 1980 || c.EndYear != 1987 {
		t.Errorf("first: %+v", c)
	}
	if c := cands[1]; c.Current || c.Disambiguation != "US singer" || c.Country != "US" {
		t.Errorf("second: %+v", c)
	}
	// A link is looked up directly; the wrong kind of link says so.
	if cands, err := s.MusicCandidates(ctx, 1, "https://musicbrainz.org/artist/"+twinB, ""); err != nil || len(cands) != 1 || cands[0].Name != "Twins" || cands[0].Type != "person" {
		t.Errorf("by link: %v %+v", err, cands)
	}
	var ref MatchRefError
	if _, err := s.MusicCandidates(ctx, 1, "https://musicbrainz.org/release-group/"+rgLive, ""); !errors.As(err, &ref) {
		t.Errorf("album link for an artist: %v", err)
	}
	if cands, err := s.MusicCandidates(ctx, 1, "eeeeeeee-0000-0000-0000-000000000000", ""); err != nil || len(cands) != 0 {
		t.Errorf("unknown id: %v %+v", err, cands)
	}
	// Albums: searched with their artist; a release's id names its release group.
	cands, err = s.MusicCandidates(ctx, 2, "", "")
	if err != nil || len(cands) != 1 || cands[0].Type != "live" || cands[0].Year != 1994 || cands[0].Artist != "Twins & Friends" || cands[0].Current {
		t.Fatalf("album candidates: %v %+v", err, cands)
	}
	if cands, err := s.MusicCandidates(ctx, 2, relOne, ""); err != nil || len(cands) != 1 || cands[0].MBID != rgLive {
		t.Errorf("by release: %v %+v", err, cands)
	}
	// Movies aren't music.
	if _, err := s.MusicCandidates(ctx, 6, "", ""); !errors.Is(err, ErrNotMusicMatchable) {
		t.Errorf("movie: %v", err)
	}
	if err := s.MusicMatchTo(ctx, 6, twinB); !errors.Is(err, ErrNotMusicMatchable) {
		t.Errorf("movie match: %v", err)
	}
	if err := s.MusicMatchTo(ctx, 4, twinB); !errors.Is(err, ErrNotMusicMatchable) {
		t.Errorf("track match: %v", err)
	}

	// Matching the artist replaces what the old match brought.
	if err := s.MusicMatchTo(ctx, 1, "https://musicbrainz.org/artist/"+twinB); err != nil {
		t.Fatal(err)
	}
	if v := str(`SELECT value FROM external_ids WHERE item_id = 1 AND provider = 'musicbrainz'`); v != twinB {
		t.Errorf("mbid: %s", v)
	}
	if v := str(`SELECT mbid FROM music_matches WHERE library_id = 1 AND kind = 'artist' AND scan_key = 'a:twins'`); v != twinB {
		t.Errorf("manual match not kept: %q", v)
	}
	if v := str(`SELECT artist_type || ' ' || gender || ' ' || country || ' ' || begin_year || ' ' || COALESCE(end_year, '-') FROM music_info WHERE item_id = 1`); v != "person female US 1990 -" {
		t.Errorf("info: %q", v)
	}
	if v := str(`SELECT summary FROM items WHERE id = 1`); v != "Twins is an American soul singer." {
		t.Errorf("bio: %q", v)
	}
	if v := tags(1, "style"); v != "neo soul,soul" {
		t.Errorf("styles: %q", v)
	}
	if v := tags(1, "genre"); v != "Soul" {
		t.Errorf("genres: %q", v)
	}
	if v := str(`SELECT group_concat(track_id) FROM music_popular WHERE artist_id = 1`); v != "3" {
		t.Errorf("popular: %q", v)
	}
	if v := str(`SELECT group_concat(remote_url || ':' || selected) FROM artwork WHERE item_id = 1`); v != "https://cdn.example/right.jpg:1" {
		t.Errorf("picture: %q", v)
	}
	if v := str(`SELECT found FROM musicbrainz_lookups WHERE item_id = 1`); v != "1" {
		t.Errorf("lookup: %q", v)
	}

	// Locked fields stay.
	if err := s.MusicMatchTo(ctx, 5, twinB); err != nil {
		t.Fatal(err)
	}
	if v := str(`SELECT summary FROM items WHERE id = 5`); v != "Written by hand." {
		t.Errorf("locked summary: %q", v)
	}
	if v := tags(5, "genre"); v != "Krautrock" {
		t.Errorf("locked genres: %q", v)
	}

	// The album: release group, type, original date and label of the first release.
	if err := s.MusicMatchTo(ctx, 2, rgLive); err != nil {
		t.Fatal(err)
	}
	if v := str(`SELECT release_type || ' ' || originally_available_at || ' ' || year FROM items WHERE id = 2`); v != "live 1994-03-01 1994" {
		t.Errorf("album: %q", v)
	}
	if v := str(`SELECT primary_type || ' ' || label FROM music_info WHERE item_id = 2`); v != "album Motown" {
		t.Errorf("album info: %q", v)
	}
	if v := str(`SELECT group_concat(provider || '=' || value) FROM external_ids WHERE item_id = 2`); v != "musicbrainz_releasegroup="+rgLive {
		t.Errorf("album ids: %q", v)
	}
	if v := tags(2, "genre"); v != "Live Rock,Soul" {
		t.Errorf("album genres (files' kept): %q", v)
	}
	if cands, _ := s.MusicCandidates(ctx, 2, "", ""); len(cands) != 1 || !cands[0].Current {
		t.Errorf("album now current: %+v", cands)
	}

	// The background lookup uses the manual match, not the tags, when it revisits.
	mustExec(t, d, `DELETE FROM music_info`)
	mustExec(t, d, `DELETE FROM musicbrainz_lookups`)
	mustExec(t, d, `UPDATE external_ids SET value = '`+twinA+`' WHERE item_id = 1 AND provider = 'musicbrainz'`)
	paths = nil
	if _, err := s.EnrichMusic(ctx); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, " ")
	if strings.Contains(joined, twinA) || !strings.Contains(joined, "/artist/"+twinB) || !strings.Contains(joined, "/release-group/"+rgLive) {
		t.Errorf("lookups: %s", joined)
	}
	if v := str(`SELECT country FROM music_info WHERE item_id = 1`); v != "US" {
		t.Errorf("revisit: %q", v)
	}
}
