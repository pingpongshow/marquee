package livetv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/settings"
)

const playlist = `#EXTM3U url-tvg="http://example/epg.xml"
#EXTINF:-1 tvg-id="news.1" tvg-name="News" tvg-logo="http://logo/news.png" tvg-chno="4" group-title="News",News One
http://streams/news
#EXTINF:-1 tvg-id="kids" tvg-chno="10" group-title="Kids, Family",Cartoons, Classic
http://streams/kids
#EXTINF:-1 tvg-chno="4.1",No Guide
#EXTGRP:Extras
http://streams/noguide
`

func TestParseM3U(t *testing.T) {
	p, err := ParseM3U(strings.NewReader(playlist))
	if err != nil {
		t.Fatal(err)
	}
	if p.EPG != "http://example/epg.xml" || len(p.Entries) != 3 {
		t.Fatalf("%+v", p)
	}
	if e := p.Entries[1]; e.Name != "Cartoons, Classic" || e.Group != "Kids, Family" || e.Number != "10" || e.URL != "http://streams/kids" {
		t.Errorf("quoted commas: %+v", e)
	}
	if e := p.Entries[2]; e.Group != "Extras" || e.TvgID != "" {
		t.Errorf("EXTGRP: %+v", e)
	}
}

func guideXML(now time.Time) string {
	f := func(t time.Time) string { return t.UTC().Format("20060102150405 +0000") }
	h := now.Truncate(time.Hour)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="news.1"><display-name>News One</display-name><icon src="http://logo/news-guide.png"/></channel>
  <channel id="kids"><display-name>Cartoons</display-name><icon src="http://logo/kids.png"/></channel>
  <programme start="%s" stop="%s" channel="news.1"><title>Morning News</title><desc>Headlines.</desc><category>News</category></programme>
  <programme start="%s" stop="%s" channel="news.1"><title>Weather</title><episode-num system="xmltv_ns">1.4.0/1</episode-num></programme>
  <programme start="%s" stop="%s" channel="kids"><title>Old Cartoon</title></programme>
  <programme start="%s" stop="%s" channel="kids"><title>Far Future</title></programme>
</tv>`, f(h), f(h.Add(time.Hour)), f(h.Add(time.Hour)), f(h.Add(90*time.Minute)),
		f(h.Add(-10*time.Hour)), f(h.Add(-9*time.Hour)), f(h.Add(72*time.Hour)), f(h.Add(73*time.Hour)))
}

func TestParseXMLTV(t *testing.T) {
	now := time.Now()
	var got []Programme
	chans, err := ParseXMLTV(strings.NewReader(guideXML(now)), now.Add(-3*time.Hour), now.Add(48*time.Hour), func(p Programme) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(chans) != 2 || chans[0].Icon != "http://logo/news-guide.png" {
		t.Errorf("channels: %+v", chans)
	}
	if len(got) != 2 {
		t.Fatalf("only programmes in the window: %+v", got)
	}
	if got[1].Episode != "S2 E5" || got[0].Category != "News" {
		t.Errorf("details: %+v", got)
	}
}

func setup(t *testing.T) (*Service, *httptest.Server, int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/output/m3u":
			io.WriteString(w, strings.Replace(playlist, "http://example/epg.xml", "", 1))
		case "/output/epg":
			io.WriteString(w, guideXML(time.Now()))
		case "/logo.png":
			w.Write([]byte("\x89PNG\r\n\x1a\nfake"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	store, _ := settings.Open(ctx, d)
	store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.LiveTVSources = []settings.LiveTVSource{{ID: "d", Name: "Dispatcharr", Kind: "dispatcharr", URL: srv.URL, Enabled: true}}
		return nil
	})
	res, _ := d.Exec(`INSERT INTO users (username, display_name) VALUES ('u', 'U')`)
	uid, _ := res.LastInsertId()
	LogoDir = filepath.Join(dir, "logos")
	return &Service{DB: d, Settings: store}, srv, uid
}

func TestRefreshChannelsAndGuide(t *testing.T) {
	s, srv, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx)
	n, until, sources := s.Status(ctx)
	if n != 3 || until == nil || len(sources) != 1 || sources[0].Error != "" || sources[0].Programmes != 2 {
		t.Fatalf("status: %d %v %+v", n, until, sources)
	}
	list, err := s.Channels(ctx, uid, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// Numbers sort as numbers: 4, 4.1, 10.
	if len(list) != 3 || list[0].Number != "4" || list[1].Number != "4.1" || list[2].Number != "10" {
		t.Fatalf("order: %+v", list)
	}
	if list[0].Now == nil || list[0].Now.Title != "Morning News" || list[0].Next == nil || list[0].Next.Title != "Weather" {
		t.Errorf("now/next: %+v %+v", list[0].Now, list[0].Next)
	}
	if list[2].Now != nil {
		t.Errorf("past programmes aren't on now: %+v", list[2].Now)
	}
	if err := s.SetFavorite(ctx, uid, list[2].ID, true); err != nil {
		t.Fatal(err)
	}
	fav, _ := s.Channels(ctx, uid, Filter{Favorites: true})
	if len(fav) != 1 || !fav[0].Favorite {
		t.Errorf("favourites: %+v", fav)
	}
	news, _ := s.Channels(ctx, uid, Filter{Group: "News"})
	if len(news) != 1 {
		t.Errorf("group: %+v", news)
	}
	now := time.Now()
	rows, _ := s.Guide(ctx, uid, Filter{}, now.Add(-time.Hour), now.Add(3*time.Hour))
	if len(rows) != 3 || len(rows[0].Programmes) != 2 {
		t.Errorf("guide: %+v", rows)
	}
	// Refreshing again keeps ids (and so favourites).
	s.Refresh(ctx)
	fav, _ = s.Channels(ctx, uid, Filter{Favorites: true})
	if len(fav) != 1 {
		t.Errorf("favourites after refresh: %+v", fav)
	}
	// Logos are proxied and cached.
	s.DB.Exec(`UPDATE live_channels SET logo_url = ? WHERE id = ?`, srv.URL+"/logo.png", list[0].ID)
	b, ct, err := s.Logo(ctx, list[0].ID)
	if err != nil || ct != "image/png" || len(b) == 0 {
		t.Errorf("logo: %s %v", ct, err)
	}
	// A removed source takes its channels with it.
	s.Settings.Update(ctx, func(x *settings.Settings) error { x.Integrations.LiveTVSources = nil; return nil })
	s.Refresh(ctx)
	if n, _, _ := s.Status(ctx); n != 0 {
		t.Errorf("channels left: %d", n)
	}
}

// TestLiveStream runs FFmpeg on a short MPEG-TS clip served over HTTP (skipped without FFmpeg).
func TestLiveStream(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	fp, _ := exec.LookPath("ffprobe")
	dir := t.TempDir()
	clip := filepath.Join(dir, "in.ts")
	// Interlaced MPEG-2 with MP2 audio, like cable TV: must be transcoded.
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc=size=720x480:rate=30", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", "12", "-vf", "setfield=tff", "-c:v", "mpeg2video", "-flags", "+ilme+ildct", "-alternate_scan", "1", "-c:a", "mp2", "-f", "mpegts", clip).CombinedOutput(); err != nil {
		t.Fatalf("making clip: %v %s", err, out)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, clip) }))
	defer srv.Close()
	m := &Sessions{FFmpeg: ff, FFprobe: fp, Dir: filepath.Join(dir, "live")}
	ctx := context.Background()
	s, err := m.Start(ctx, 1, 1, srv.URL+"/in.ts", Options{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Method != "transcode" || s.Encoder != "software" {
		t.Errorf("decision: %+v", s)
	}
	web := httptest.NewServer(m.Handler())
	defer web.Close()
	resp, err := http.Get(web.URL + "/api/v1/live/" + s.ID + "/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "#EXTINF") || strings.Contains(string(body), "#EXT-X-ENDLIST") {
		t.Fatalf("playlist (%d): %s", resp.StatusCode, body)
	}
	seg := ""
	for _, l := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(l, ".ts") {
			seg = l
			break
		}
	}
	resp, _ = http.Get(web.URL + "/api/v1/live/" + s.ID + "/" + seg)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(b) < 1000 {
		t.Errorf("segment %s: %d, %d bytes", seg, resp.StatusCode, len(b))
	}
	// Other paths and other sessions aren't served.
	for _, p := range []string{"/api/v1/live/" + s.ID + "/../../etc/passwd", "/api/v1/live/nope/index.m3u8"} {
		resp, _ = http.Get(web.URL + p)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Errorf("%s served", p)
		}
	}
	if !m.Stop(s.ID) {
		t.Error("stop")
	}
	if _, err := os.Stat(filepath.Join(m.Dir, s.ID)); !os.IsNotExist(err) {
		t.Error("files left behind")
	}
}

func TestHiddenRecentAndGroups(t *testing.T) {
	s, _, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx)
	all, _ := s.Channels(ctx, uid, Filter{})
	if len(all) != 3 {
		t.Fatalf("channels: %d", len(all))
	}
	// Hidden channels leave the guide and are listed on their own.
	if err := s.SetHidden(ctx, uid, all[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.Channels(ctx, uid, Filter{}); len(l) != 2 {
		t.Errorf("hidden still listed: %d", len(l))
	}
	if l, _ := s.Channels(ctx, uid, Filter{Hidden: true}); len(l) != 1 || !l[0].Hidden {
		t.Errorf("hidden list: %+v", l)
	}
	now := time.Now()
	if rows, _ := s.Guide(ctx, uid, Filter{}, now.Add(-time.Hour), now.Add(time.Hour)); len(rows) != 2 {
		t.Errorf("guide rows: %d", len(rows))
	}
	s.SetHidden(ctx, uid, all[0].ID, false)
	if err := s.SetHidden(ctx, uid, 9999, true); err != ErrNotFound {
		t.Errorf("unknown channel: %v", err)
	}
	// Recently watched, newest first.
	s.Watched(ctx, uid, all[2].ID)
	time.Sleep(5 * time.Millisecond)
	s.Watched(ctx, uid, all[0].ID)
	if l, _ := s.Channels(ctx, uid, Filter{Recent: true}); len(l) != 2 || l[0].ID != all[0].ID || l[1].ID != all[2].ID {
		t.Errorf("recent: %+v", l)
	}
	// A restricted profile sees only its groups; an empty list means none.
	kids := []string{"Kids, Family"}
	if l, _ := s.Channels(ctx, uid, Filter{Groups: &kids}); len(l) != 1 || l[0].Group != "Kids, Family" {
		t.Errorf("groups: %+v", l)
	}
	if l, _ := s.Channels(ctx, uid, Filter{Groups: &[]string{}}); len(l) != 0 {
		t.Errorf("no live tv: %+v", l)
	}
}
