package speakers

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/speakers/speakerstest"
)

const roomID = "RINCON_TEST01400"

type fixture struct {
	s      *Service
	p      *speakerstest.Player
	db     *sql.DB
	user   int64
	dir    string
	tracks []Track
	routes http.Handler
	ctx    context.Context
}

// newFixture is a service with one room on a fake player, and a library of songs: three MP3s
// (the first with awkward characters), and a 96 kHz FLAC. The poll loop is out of the way, so
// a test moves things along with Get.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := database.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	user := exec(`INSERT INTO users (username, display_name) VALUES ('u', 'U')`)
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('Music', 'music')`)
	addTrack := func(title, codec, file string, rate, bits int) Track {
		item := exec(`INSERT INTO items (library_id, type, title, sort_title, duration_ms) VALUES (?, 'track', ?, ?, 60000)`, lib, title, title)
		v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte("audio of "+file+strings.Repeat(".", 1000)), 0o644); err != nil {
			t.Fatal(err)
		}
		f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, available) VALUES (?, ?, ?, 1, 1, 60000, 1)`, v, lib, path)
		exec(`INSERT INTO streams (file_id, stream_index, kind, codec, sample_rate, bit_depth, channels) VALUES (?, 0, 'audio', ?, ?, ?, 2)`, f, codec, rate, bits)
		return Track{ItemID: item, Title: title, Artist: "Artist", Album: "Album", DurationMS: 60000}
	}
	tracks := []Track{
		addTrack(`Rock & Roll "Live" <1>`, "mp3", "a.mp3", 44100, 0),
		addTrack("Two", "mp3", "b.mp3", 44100, 0),
		addTrack("Three", "mp3", "c.mp3", 44100, 0),
		addTrack("Hi-Res", "flac", "d.flac", 96000, 24),
	}
	p := speakerstest.New(t, roomID)
	p.Set(func(p *speakerstest.Player) { p.Topology = p.RoomTopology("Den & Study") })
	grace := time.Duration(0)
	s := &Service{DB: database, Dir: filepath.Join(dir, "speakers"), Port: 32500, Hosts: []string{p.Addr}, PollEvery: time.Hour, Grace: &grace,
		Convert: func(ctx context.Context, userID, itemID, fileID int64, sampleRate int, out string) error {
			return os.WriteFile(out, []byte("converted"), 0o644)
		}}
	mux := http.NewServeMux()
	mux.Handle(StreamRoute, s.Handler())
	mux.Handle(ArtRoute, s.Handler())
	t.Cleanup(func() {
		for _, ss := range s.liveSessions() {
			ss.mu.Lock()
			ss.finish("the test is over")
			ss.mu.Unlock()
		}
	})
	return &fixture{s: s, p: p, db: database, user: user, dir: dir, tracks: tracks, routes: mux, ctx: ctx}
}

func (f *fixture) get(t *testing.T) Status {
	t.Helper()
	st, err := f.s.Get(f.ctx, roomID)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// playing has the fake report a position, as the song it's on goes along.
func (f *fixture) at(rel, dur string) {
	f.p.Set(func(p *speakerstest.Player) { p.Rel, p.Duration = rel, dur })
}

func (f *fixture) fetch(path string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	f.routes.ServeHTTP(w, r)
	return w
}

func pathOf(t *testing.T, uri string) string {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	return u.Path
}

func TestParseTopologyHidesSatellites(t *testing.T) {
	// The shape of S2's answer: a home theatre with surrounds, a stereo pair, a Boost, and two
	// rooms grouped together.
	rooms, addrs, err := parseTopology(`<ZoneGroupState><ZoneGroups>
		<ZoneGroup Coordinator="ARC" ID="ARC:1"><ZoneGroupMember UUID="ARC" Location="http://10.0.0.1:1400/xml/device_description.xml" ZoneName="Living Room" HTSatChanMapSet="x">
			<Satellite UUID="SL1" Location="http://10.0.0.2:1400/xml/device_description.xml" ZoneName="Living Room" Invisible="1"/>
			<Satellite UUID="SL2" Location="http://10.0.0.3:1400/xml/device_description.xml" ZoneName="Living Room" Invisible="1"/>
		</ZoneGroupMember></ZoneGroup>
		<ZoneGroup Coordinator="LEFT" ID="LEFT:1">
			<ZoneGroupMember UUID="LEFT" Location="http://10.0.0.4:1400/x" ZoneName="Kitchen" ChannelMapSet="LEFT:LF,LF;RIGHT:RF,RF"/>
			<ZoneGroupMember UUID="RIGHT" Location="http://10.0.0.5:1400/x" ZoneName="Kitchen" Invisible="1"/>
		</ZoneGroup>
		<ZoneGroup Coordinator="BOOST" ID="BOOST:1"><ZoneGroupMember UUID="BOOST" Location="http://10.0.0.6:1400/x" ZoneName="BOOST" IsZoneBridge="1"/></ZoneGroup>
		<ZoneGroup Coordinator="DEN" ID="DEN:1">
			<ZoneGroupMember UUID="DEN" Location="http://10.0.0.7:1400/x" ZoneName="Den"/>
			<ZoneGroupMember UUID="HALL" Location="http://10.0.0.8:1400/x" ZoneName="Hall &amp; Stairs"/>
		</ZoneGroup>
		</ZoneGroups><VanishedDevices></VanishedDevices></ZoneGroupState>`, localPlayerAddr)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rooms {
		names = append(names, r.Name)
	}
	if want := []string{"Living Room", "Kitchen", "Den", "Hall & Stairs"}; !slices.Equal(names, want) {
		t.Fatalf("rooms %v, want %v", names, want)
	}
	if h := rooms[3]; h.ID != "HALL" || h.Addr != "10.0.0.8:1400" || h.Coordinator != "DEN" || h.CoordinatorAddr != "10.0.0.7:1400" {
		t.Fatalf("a grouped room commands its coordinator: %+v", h)
	}
	if addrs["SL1"] != "10.0.0.2:1400" {
		t.Fatalf("addresses %v", addrs)
	}
	// Older software starts at ZoneGroups.
	rooms, _, err = parseTopology(`<ZoneGroups><ZoneGroup Coordinator="A"><ZoneGroupMember UUID="A" Location="http://10.0.0.9:1400/x" ZoneName="Old"/></ZoneGroup></ZoneGroups>`, localPlayerAddr)
	if err != nil || len(rooms) != 1 || rooms[0].Name != "Old" {
		t.Fatalf("older topology: %+v %v", rooms, err)
	}
}

func TestSearchFindsPlayersOnly(t *testing.T) {
	answer := func(t *testing.T, msg string) string {
		responder, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Skip("no UDP here:", err)
		}
		defer responder.Close()
		go func() {
			buf := make([]byte, 2048)
			n, from, err := responder.ReadFrom(buf)
			if err != nil || !strings.Contains(string(buf[:n]), "ST: "+searchTarget) {
				return
			}
			responder.WriteTo([]byte(msg), from)
		}()
		pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer pc.Close()
		return strings.Join(searchOn(context.Background(), pc, responder.LocalAddr(), 200*time.Millisecond), ",")
	}
	// The location in the answer is ignored: only the sender is ever contacted.
	player := "HTTP/1.1 200 OK\r\nLOCATION: http://203.0.113.9:1400/xml/device_description.xml\r\nst: " + searchTarget + "\r\n\r\n"
	if got := answer(t, player); got != "127.0.0.1:1400" {
		t.Fatalf("player answer: %q", got)
	}
	if got := answer(t, "HTTP/1.1 200 OK\r\nST: upnp:rootdevice\r\n\r\n"); got != "" {
		t.Fatalf("someone else's answer: %q", got)
	}
}

func TestNoPlayersIsAnEmptyList(t *testing.T) {
	s := &Service{search: func(context.Context) []string { return nil }}
	if got := s.List(context.Background()); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	if _, err := s.Get(context.Background(), "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get: %v", err)
	}
}

func TestListingReadsTheTopologyAndModel(t *testing.T) {
	f := newFixture(t)
	f.p.Set(func(p *speakerstest.Player) { p.Volume = 44 })
	got := f.s.List(f.ctx)
	if len(got) != 1 || got[0].ID != roomID || got[0].Name != "Den & Study" || got[0].Model != "Sonos Fake" || got[0].State != "stopped" || got[0].Volume != 44 || got[0].Marquee {
		t.Fatalf("%+v", got)
	}
}

// Hosts is a hard limit: a room led by a player outside them isn't listed or commanded, and
// that player hears nothing, even when another player reports it.
func TestHostsLimitWhoIsContacted(t *testing.T) {
	f := newFixture(t)
	other := speakerstest.New(t, "RINCON_OTHER")
	f.p.Set(func(p *speakerstest.Player) {
		p.Topology = `<ZoneGroupState><ZoneGroups>
			<ZoneGroup Coordinator="` + roomID + `"><ZoneGroupMember UUID="` + roomID + `" Location="http://` + p.Addr + `/x" ZoneName="Mine"/></ZoneGroup>
			<ZoneGroup Coordinator="RINCON_OTHER"><ZoneGroupMember UUID="RINCON_OTHER" Location="http://` + other.Addr + `/x" ZoneName="Not mine"/>
				<ZoneGroupMember UUID="RINCON_JOINED" Location="http://` + p.Addr + `/x" ZoneName="Led from elsewhere"/></ZoneGroup>
			</ZoneGroups></ZoneGroupState>`
	})
	got := f.s.List(f.ctx)
	if len(got) != 1 || got[0].Name != "Mine" {
		t.Fatalf("%+v", got)
	}
	if _, err := f.s.Pause(f.ctx, "RINCON_OTHER"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := f.s.soap(f.ctx, other.Addr, avTransport, "Stop"); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("soap: %v", err)
	}
	if calls := other.Actions(); len(calls) != 0 {
		t.Fatalf("it heard %v", calls)
	}
}

func TestMetadataIsEscaped(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:2], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	c, ok := f.p.Last("SetAVTransportURI")
	if !ok {
		t.Fatal("no SetAVTransportURI")
	}
	var d struct {
		Item struct {
			Title   string `xml:"title"`
			Creator string `xml:"creator"`
			Album   string `xml:"album"`
			Res     struct {
				URL      string `xml:",chardata"`
				Protocol string `xml:"protocolInfo,attr"`
			} `xml:"res"`
		} `xml:"item"`
	}
	if err := xml.Unmarshal([]byte(c.Args["CurrentURIMetaData"]), &d); err != nil {
		t.Fatalf("%v in %s", err, c.Args["CurrentURIMetaData"])
	}
	if d.Item.Title != `Rock & Roll "Live" <1>` || d.Item.Creator != "Artist" || d.Item.Album != "Album" {
		t.Fatalf("%+v", d.Item)
	}
	if d.Item.Res.URL != c.Args["CurrentURI"] || d.Item.Res.Protocol != "http-get:*:audio/mpeg:*" {
		t.Fatalf("res %+v, uri %s", d.Item.Res, c.Args["CurrentURI"])
	}
}

func TestEscapingOfSoapValues(t *testing.T) {
	if got := esc(`a & b "c" <d>`); got != `a &amp; b &#34;c&#34; &lt;d&gt;` {
		t.Fatal(got)
	}
	f := newFixture(t)
	f.s.init()
	if _, err := f.s.soap(f.ctx, f.p.Addr, avTransport, "SetAVTransportURI", "CurrentURI", `http://x/?a=1&b="2"`, "CurrentURIMetaData", `<a b="c&d"/>`); err != nil {
		t.Fatal(err)
	}
	c, _ := f.p.Last("SetAVTransportURI")
	if c.Args["CurrentURI"] != `http://x/?a=1&b="2"` || c.Args["CurrentURIMetaData"] != `<a b="c&d"/>` {
		t.Fatalf("%+v", c.Args)
	}
}

func TestQueueFollowsTheSpeaker(t *testing.T) {
	f := newFixture(t)
	st, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:3], 0, 0, "mix:daily")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Marquee || st.ItemID != f.tracks[0].ItemID || st.QueueIndex != 0 || st.QueueLength != 3 {
		t.Fatalf("%+v", st)
	}
	if got := f.p.Get(); got.State != "PLAYING" || !strings.Contains(got.URI, fmt.Sprintf("/%d.mp3", f.tracks[0].ItemID)) {
		t.Fatalf("%+v", got)
	}

	// A poll tells the speaker what comes next.
	f.at("0:00:05", "0:01:00")
	st = f.get(t)
	if st.State != "playing" || st.PositionMS != 5000 || st.DurationMS != 60000 {
		t.Fatalf("%+v", st)
	}
	if got := f.p.Get(); !strings.Contains(got.NextURI, fmt.Sprintf("/%d.mp3", f.tracks[1].ItemID)) || !strings.Contains(got.NextMeta, "<dc:title>Two</dc:title>") {
		t.Fatalf("next: %+v", got)
	}

	// The speaker moves on by itself: gapless, with no new SetAVTransportURI.
	f.at("0:00:59", "0:01:00")
	f.get(t)
	f.p.Finish()
	st = f.get(t)
	if st.QueueIndex != 1 || st.ItemID != f.tracks[1].ItemID || !st.Marquee {
		t.Fatalf("after the first song: %+v", st)
	}
	if n := countOf(f.p.Actions(), "SetAVTransportURI"); n != 1 {
		t.Fatalf("%d SetAVTransportURI", n)
	}
	if got := f.p.Get(); !strings.Contains(got.NextURI, fmt.Sprintf("/%d.mp3", f.tracks[2].ItemID)) {
		t.Fatalf("next after moving: %+v", got)
	}

	// Next uses what the speaker was told; then the end of the queue ends the session.
	if st, err = f.s.Next(f.ctx, roomID); err != nil || st.QueueIndex != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	if n := countOf(f.p.Actions(), "Next"); n != 1 {
		t.Fatalf("%d Next", n)
	}
	f.at("0:00:58", "0:01:00")
	f.get(t)
	token := tokenOf(f.p.Get().URI)
	f.p.Finish() // stops: nothing was next
	stopsBefore := countOf(f.p.Actions(), "Stop")
	st = f.get(t)
	if st.Marquee || st.State != "stopped" || st.ItemID != 0 {
		t.Fatalf("at the end: %+v", st)
	}
	if countOf(f.p.Actions(), "Stop") != stopsBefore {
		t.Fatal("the end of the queue shouldn't send Stop")
	}
	if w := f.fetch(streamPrefix + token + fmt.Sprintf("/%d.mp3", f.tracks[2].ItemID)); w.Code != http.StatusNotFound {
		t.Fatalf("after the session: %d", w.Code)
	}

	// The two songs played out are in the history as Sonos plays, with their context; the one
	// skipped early isn't.
	rows, err := f.db.Query(`SELECT item_id, source, context, position_ms, stopped_at IS NOT NULL FROM play_history WHERE user_id = ? ORDER BY id`, f.user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id, pos int64
		var src, label string
		var stopped bool
		rows.Scan(&id, &src, &label, &pos, &stopped)
		got = append(got, fmt.Sprint(id, src, label, pos >= 30000, stopped))
	}
	want := []string{fmt.Sprint(f.tracks[0].ItemID, "sonos", "mix:daily", true, true), fmt.Sprint(f.tracks[2].ItemID, "sonos", "mix:daily", true, true)}
	if !slices.Equal(got, want) {
		t.Fatalf("history %v, want %v", got, want)
	}
	var plays int
	f.db.QueryRow(`SELECT play_count FROM user_item_state WHERE user_id = ? AND item_id = ?`, f.user, f.tracks[0].ItemID).Scan(&plays)
	if plays != 1 {
		t.Fatalf("play count %d", plays)
	}
}

func countOf(list []string, v string) int {
	n := 0
	for _, s := range list {
		if s == v {
			n++
		}
	}
	return n
}

func tokenOf(uri string) string {
	_, rest, _ := strings.Cut(uri, streamPrefix)
	token, _, _ := strings.Cut(rest, "/")
	return token
}

// A play counts once, at half way, and not before.
func TestPlayCountsAtThreshold(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:1], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	count := func() (n int) {
		f.db.QueryRow(`SELECT COUNT(*) FROM play_history WHERE user_id = ?`, f.user).Scan(&n)
		return n
	}
	f.at("0:00:29", "0:01:00")
	f.get(t)
	if n := count(); n != 0 {
		t.Fatalf("before half way: %d", n)
	}
	f.at("0:00:30", "0:01:00")
	f.get(t)
	f.at("0:00:40", "0:01:00")
	f.get(t)
	if n := count(); n != 1 {
		t.Fatalf("past half way: %d", n)
	}
}

func TestSpeakerThatWontDoGaplessStillMovesOn(t *testing.T) {
	f := newFixture(t)
	f.p.Set(func(p *speakerstest.Player) { p.Faults["SetNextAVTransportURI"] = 714 })
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:2], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	f.at("0:00:59", "0:01:00")
	f.get(t)
	f.p.Finish()
	st := f.get(t)
	if !st.Marquee || st.QueueIndex != 1 {
		t.Fatalf("%+v", st)
	}
	got := f.p.Get()
	if got.State != "PLAYING" || !strings.Contains(got.URI, fmt.Sprintf("/%d.mp3", f.tracks[1].ItemID)) {
		t.Fatalf("%+v", got)
	}
	if n := countOf(f.p.Actions(), "SetNextAVTransportURI"); n != 1 {
		t.Fatalf("asked %d times", n)
	}
}

func TestAnotherAppTakingOverEndsTheSession(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:2], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	f.at("0:00:05", "0:01:00")
	f.get(t)
	item := fmt.Sprintf("/%d.mp3", f.tracks[0].ItemID)
	path := pathOf(t, f.p.Get().URI)
	if w := f.fetch(path); w.Code != http.StatusOK {
		t.Fatalf("while live: %d", w.Code)
	}
	actions := len(f.p.Actions("GetTransportInfo", "GetPositionInfo", "GetVolume"))
	f.p.Set(func(p *speakerstest.Player) { p.URI = "x-sonos-spotify:spotify%3atrack%3a1?sid=9" })
	st := f.get(t)
	if st.Marquee || st.ItemID != 0 || st.QueueLength != 0 || st.State != "playing" {
		t.Fatalf("%+v", st)
	}
	if got := len(f.p.Actions("GetTransportInfo", "GetPositionInfo", "GetVolume")); got != actions {
		t.Fatalf("it interfered: %v", f.p.Actions())
	}
	if w := f.fetch(path); w.Code != http.StatusNotFound || !strings.HasSuffix(path, item) {
		t.Fatalf("after the takeover %s: %d", path, w.Code)
	}
	// What's playing is someone else's: skip and seek don't apply, the rest do.
	if _, err := f.s.Next(f.ctx, roomID); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("Next: %v", err)
	}
	if _, err := f.s.Seek(f.ctx, roomID, 1000); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("Seek: %v", err)
	}
	if _, err := f.s.Add(f.ctx, roomID, f.tracks[:1]); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("Add: %v", err)
	}
	if st, err := f.s.Pause(f.ctx, roomID); err != nil || st.State != "paused" {
		t.Fatalf("Pause: %+v %v", st, err)
	}
	if st, err := f.s.SetVolume(f.ctx, roomID, 12); err != nil || st.Volume != 12 {
		t.Fatalf("SetVolume: %+v %v", st, err)
	}
	if st, err := f.s.Stop(f.ctx, roomID); err != nil || st.State != "stopped" {
		t.Fatalf("Stop: %+v %v", st, err)
	}
}

func TestStoppedInTheSonosAppEndsTheSession(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:2], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	f.at("0:00:05", "0:01:00")
	f.get(t)
	f.p.Set(func(p *speakerstest.Player) { p.State = "STOPPED" })
	if st := f.get(t); st.Marquee {
		t.Fatalf("%+v", st)
	}
}

func TestControls(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Next(f.ctx, roomID); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("Next with nothing playing: %v", err)
	}
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:3], 1, 20000, ""); err != nil {
		t.Fatal(err)
	}
	if c, ok := f.p.Last("Seek"); !ok || c.Args["Target"] != "0:00:20" || c.Args["Unit"] != "REL_TIME" {
		t.Fatalf("start position: %+v", c)
	}
	f.at("0:00:20", "0:01:00")
	if st, err := f.s.Pause(f.ctx, roomID); err != nil || st.State != "paused" || !st.Marquee {
		t.Fatalf("Pause: %+v %v", st, err)
	}
	if st, err := f.s.Resume(f.ctx, roomID); err != nil || st.State != "playing" {
		t.Fatalf("Resume: %+v %v", st, err)
	}
	if st, err := f.s.Seek(f.ctx, roomID, 45000); err != nil || st.PositionMS != 45000 {
		t.Fatalf("Seek: %+v %v", st, err)
	}
	// Past the end of the song stops short of it.
	if _, err := f.s.Seek(f.ctx, roomID, 999000); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.p.Last("Seek"); c.Args["Target"] != "0:00:59" {
		t.Fatalf("clamped to %+v", c)
	}
	// Previous restarts a song well under way, then goes back.
	if st, err := f.s.Previous(f.ctx, roomID); err != nil || st.QueueIndex != 1 {
		t.Fatalf("Previous: %+v %v", st, err)
	}
	if c, _ := f.p.Last("Seek"); c.Args["Target"] != "0:00:00" {
		t.Fatalf("restart: %+v", c)
	}
	if st, err := f.s.Previous(f.ctx, roomID); err != nil || st.QueueIndex != 0 || st.ItemID != f.tracks[0].ItemID {
		t.Fatalf("Previous: %+v %v", st, err)
	}
	// A speaker refusing is a conflict, not an outage.
	f.p.Set(func(p *speakerstest.Player) { p.Faults["Seek"] = 711 })
	if _, err := f.s.Seek(f.ctx, roomID, 1000); !errors.Is(err, ErrRefused) {
		t.Fatalf("refused Seek: %v", err)
	}
	// Past the last song, it ends.
	f.get(t)
	if st, err := f.s.Stop(f.ctx, roomID); err != nil || st.Marquee || f.p.Get().State != "STOPPED" {
		t.Fatalf("Stop: %+v %v", st, err)
	}
}

func TestAddToQueue(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:1], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	st, err := f.s.Add(f.ctx, roomID, f.tracks[1:2])
	if err != nil || st.QueueLength != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	f.at("0:00:01", "0:01:00")
	f.get(t)
	if got := f.p.Get(); !strings.Contains(got.NextURI, fmt.Sprintf("/%d.mp3", f.tracks[1].ItemID)) {
		t.Fatalf("%+v", got)
	}
	// At the limit, what has been played goes first.
	ss := f.s.session(roomID)
	ss.mu.Lock()
	ss.queue = append(ss.queue, slices.Repeat(f.tracks[2:3], MaxQueue-2)...)
	ss.index = 5
	ss.add(f.tracks[:3])
	if len(ss.queue) != MaxQueue || ss.index != 2 || ss.queue[MaxQueue-1].ItemID != f.tracks[2].ItemID {
		t.Fatalf("%d songs, at %d", len(ss.queue), ss.index)
	}
	ss.mu.Unlock()
}

func TestFormatDecisions(t *testing.T) {
	for _, tt := range []struct {
		name string
		f    sourceFile
		ext  string
		hz   int
	}{
		{"mp3", sourceFile{Path: "a.mp3", Codec: "mp3", Rate: 44100, Channels: 2}, ".mp3", 0},
		{"mp3 unknown rate", sourceFile{Path: "a.MP3", Codec: "mp3"}, ".mp3", 48000},
		{"aac", sourceFile{Path: "a.m4a", Codec: "aac", Rate: 44100, Channels: 2}, ".m4a", 0},
		{"aac stream", sourceFile{Path: "a.aac", Codec: "aac", Rate: 44100, Channels: 2}, ".aac", 0},
		{"flac 24/48", sourceFile{Path: "a.flac", Codec: "flac", Rate: 48000, Bits: 24, Channels: 2}, ".flac", 0},
		{"alac 16/44", sourceFile{Path: "a.m4a", Codec: "alac", Rate: 44100, Bits: 16, Channels: 2}, ".m4a", 0},
		{"flac 96", sourceFile{Path: "a.flac", Codec: "flac", Rate: 96000, Bits: 24, Channels: 2}, "", 48000},
		{"flac 88", sourceFile{Path: "a.flac", Codec: "flac", Rate: 88200, Bits: 24, Channels: 2}, "", 44100},
		{"flac 192", sourceFile{Path: "a.flac", Codec: "flac", Rate: 192000, Channels: 2}, "", 48000},
		{"flac 32 bit", sourceFile{Path: "a.flac", Codec: "flac", Rate: 44100, Bits: 32, Channels: 2}, "", 0},
		{"flac unknown rate", sourceFile{Path: "a.flac", Codec: "flac"}, "", 48000},
		{"alac 96", sourceFile{Path: "a.m4a", Codec: "alac", Rate: 96000, Bits: 24, Channels: 2}, "", 48000},
		{"flac 5.1", sourceFile{Path: "a.flac", Codec: "flac", Rate: 48000, Channels: 6}, "", 48000},
		{"aac 5.1", sourceFile{Path: "a.m4a", Codec: "aac", Rate: 44100, Channels: 6}, "", 44100},
		{"opus", sourceFile{Path: "a.opus", Codec: "opus", Rate: 48000, Channels: 2}, "", 0},
		{"vorbis", sourceFile{Path: "a.ogg", Codec: "vorbis", Rate: 44100, Channels: 2}, "", 0},
		{"dsd", sourceFile{Path: "a.dsf", Codec: "dsd_lsbf", Rate: 352800, Channels: 2}, "", 44100},
		{"flac in matroska", sourceFile{Path: "a.mka", Codec: "flac", Rate: 44100, Channels: 2}, "", 0},
	} {
		ext, _, ok := playable(tt.f)
		if ok != (tt.ext != "") || ext != tt.ext {
			t.Errorf("%s: playable %q %v", tt.name, ext, ok)
		}
		if hz := conversionRate(tt.f); hz != tt.hz {
			t.Errorf("%s: converted at %d, want %d", tt.name, hz, tt.hz)
		}
	}
}

func TestConversionGoesAheadAndIsCleanedUp(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	converted := make(chan int64, 4)
	var rate int
	f.s.Convert = func(ctx context.Context, userID, itemID, fileID int64, sampleRate int, out string) error {
		if userID != f.user || fileID == 0 {
			t.Errorf("converted for user %d, file %d", userID, fileID)
		}
		rate = sampleRate
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		converted <- itemID
		return os.WriteFile(out, []byte("converted"), 0o644)
	}
	hires, plain := f.tracks[3], f.tracks[1]

	// A hi-res song first: nothing to fetch yet, so the speaker is told nothing but to be quiet.
	f.p.Set(func(p *speakerstest.Player) { p.State, p.URI = "PLAYING", "x-sonos-spotify:elsewhere" })
	st, err := f.s.Play(f.ctx, roomID, f.user, []Track{hires, plain}, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "loading" || !st.Marquee || st.ItemID != hires.ItemID {
		t.Fatalf("%+v", st)
	}
	if got := f.p.Get(); got.State != "STOPPED" || got.URI != "x-sonos-spotify:elsewhere" {
		t.Fatalf("%+v", got)
	}
	ss := f.s.session(roomID)
	dir := ss.dir
	token := ss.token
	file := fmt.Sprintf("/%d.m4a", hires.ItemID)
	if w := f.fetch(streamPrefix + token + file); w.Code != http.StatusNotFound {
		t.Fatalf("before it's ready: %d", w.Code)
	}
	close(release)
	if id := <-converted; id != hires.ItemID {
		t.Fatalf("converted %d", id)
	}
	if rate != 48000 {
		t.Fatalf("at %d Hz", rate)
	}
	waitFor(t, func() bool {
		st := f.get(t)
		return strings.Contains(f.p.Get().URI, file) && st.State != "loading" || f.p.Get().State == "PLAYING"
	})
	c, _ := f.p.Last("SetAVTransportURI")
	if !strings.Contains(c.Args["CurrentURIMetaData"], "audio/mp4") {
		t.Fatalf("%s", c.Args["CurrentURIMetaData"])
	}
	if w := f.fetch(streamPrefix + token + file); w.Code != http.StatusOK || w.Body.String() != "converted" || w.Header().Get("Content-Type") != "audio/mp4" {
		t.Fatalf("ready: %d %q", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%d.m4a", hires.ItemID))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Stop(f.ctx, roomID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("conversions left behind: %v", err)
	}
}

// The next song's conversion starts as soon as the one before starts playing, and a song it
// can't prepare is skipped.
func TestNextSongIsConvertedAheadOfTime(t *testing.T) {
	f := newFixture(t)
	started := make(chan int64, 1)
	f.s.Convert = func(ctx context.Context, userID, itemID, fileID int64, sampleRate int, out string) error {
		started <- itemID
		return errors.New("no")
	}
	st, err := f.s.Play(f.ctx, roomID, f.user, []Track{f.tracks[0], f.tracks[3], f.tracks[1]}, 0, 0, "")
	if err != nil || st.State == "loading" {
		t.Fatalf("%+v %v", st, err)
	}
	select {
	case id := <-started:
		if id != f.tracks[3].ItemID {
			t.Fatalf("converted %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no conversion started")
	}
	f.at("0:00:01", "0:01:00")
	waitFor(t, func() bool {
		f.get(t)
		return strings.Contains(f.p.Get().NextURI, fmt.Sprintf("/%d.mp3", f.tracks[1].ItemID))
	})
	if st := f.get(t); st.QueueLength != 2 {
		t.Fatalf("%+v", st)
	}
}

func TestOldConversionsAreClearedAtStart(t *testing.T) {
	f := newFixture(t)
	left := filepath.Join(f.s.Dir, "old-session", "1.m4a")
	os.MkdirAll(filepath.Dir(left), 0o755)
	os.WriteFile(left, []byte("x"), 0o644)
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})
	go func() { f.s.Run(ctx); close(done) }()
	waitFor(t, func() bool { _, err := os.Stat(left); return errors.Is(err, os.ErrNotExist) })
	cancel()
	<-done
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 500 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestStreamRouteToken(t *testing.T) {
	f := newFixture(t)
	f.s.Art = func(ctx context.Context, artworkID int64, width int) (string, string, error) {
		if artworkID != 77 {
			t.Errorf("artwork %d", artworkID)
		}
		p := filepath.Join(f.dir, "art.jpg")
		return p, "image/jpeg", os.WriteFile(p, []byte("jpeg"), 0o644)
	}
	first := f.tracks[0]
	first.ArtworkID = 77
	if _, err := f.s.Play(f.ctx, roomID, f.user, []Track{first}, 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	uri := f.p.Get().URI
	token := tokenOf(uri)
	if len(token) != 32 {
		t.Fatalf("token %q", token)
	}
	path := pathOf(t, uri)
	if !strings.HasPrefix(path, streamPrefix+token+"/") || !strings.HasSuffix(path, ".mp3") {
		t.Fatalf("path %s", path)
	}

	w := f.fetch(path)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "audio/mpeg" || !strings.HasPrefix(w.Body.String(), "audio of a.mp3") {
		t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body.String())
	}
	w = f.fetch(path, "Range", "bytes=6-13")
	if w.Code != http.StatusPartialContent || w.Body.String() != "of a.mp3" || w.Header().Get("Content-Range") == "" {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}

	// Only this token, and only this session's songs.
	other := fmt.Sprintf("%d.mp3", f.tracks[1].ItemID)
	for name, bad := range map[string]string{
		"unknown token":      streamPrefix + strings.Repeat("0", 32) + "/" + filepath.Base(path),
		"song not in queue":  streamPrefix + token + "/" + other,
		"wrong extension":    strings.TrimSuffix(path, ".mp3") + ".flac",
		"not a number":       streamPrefix + token + "/x.mp3",
		"traversal":          streamPrefix + token + "/..%2f..%2fa.mp3",
		"art not in queue":   artPrefix + token + "/" + other,
		"art with bad token": artPrefix + "nope/" + filepath.Base(path),
	} {
		if w := f.fetch(bad); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", name, w.Code)
		}
	}

	art := artPrefix + token + fmt.Sprintf("/%d.jpg", first.ItemID)
	if meta := f.p.Get().Meta; !strings.Contains(meta, art) {
		t.Fatalf("no artwork in %s", meta)
	}
	if w := f.fetch(art); w.Code != http.StatusOK || w.Body.String() != "jpeg" {
		t.Fatalf("art: %d %q", w.Code, w.Body.String())
	}

	if _, err := f.s.Stop(f.ctx, roomID); err != nil {
		t.Fatal(err)
	}
	if w := f.fetch(path); w.Code != http.StatusNotFound {
		t.Fatalf("after stop: %d", w.Code)
	}
	if w := f.fetch(art); w.Code != http.StatusNotFound {
		t.Fatalf("art after stop: %d", w.Code)
	}
}

func TestUnreachableSpeaker(t *testing.T) {
	f := newFixture(t)
	f.get(t) // known
	f.p.Srv.Close()
	if _, err := f.s.Get(f.ctx, roomID); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Get: %v", err)
	}
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:1], 0, 0, ""); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Play: %v", err)
	}
	if _, err := f.s.Pause(f.ctx, roomID); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Pause: %v", err)
	}
	if got := f.s.List(f.ctx); len(got) != 0 {
		t.Fatalf("List: %+v", got)
	}
}

// A satellite answers AVTransport with a bare HTTP 500, which isn't a player saying no.
func TestBareServerErrorIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := &Service{}
	s.init()
	_, err := s.soap(context.Background(), srv.Listener.Addr().String(), avTransport, "Play", "InstanceID", "0")
	if !errors.Is(err, ErrUnreachable) {
		t.Fatal(err)
	}
}

func TestClock(t *testing.T) {
	if got := clock(3723000); got != "1:02:03" {
		t.Fatal(got)
	}
	if got := clock(-5); got != "0:00:00" {
		t.Fatal(got)
	}
	for in, want := range map[string]int64{"0:03:20": 200000, "1:00:00.500": 3600500, "NOT_IMPLEMENTED": 0, "": 0} {
		if got := parseClock(in); got != want {
			t.Errorf("%q: %d", in, got)
		}
	}
}

func TestDropUserEndsTheirSessions(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:1], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	f.s.DropUser(f.ctx, f.user+1)
	if st := f.get(t); !st.Marquee {
		t.Fatal("someone else's")
	}
	f.s.DropUser(f.ctx, f.user)
	if st := f.get(t); st.Marquee || st.State != "stopped" {
		t.Fatalf("%+v", st)
	}
}

// A failed or abandoned refresh leaves the rooms alone, so a room that's playing stays a room.
func TestFailedRefreshKeepsTheRooms(t *testing.T) {
	f := newFixture(t)
	f.get(t)
	stale := func() {
		f.s.mu.Lock()
		f.s.refreshed = time.Now().Add(-time.Hour)
		f.s.mu.Unlock()
	}
	// A request that has gone away doesn't take the refresh with it.
	f.p.Set(func(p *speakerstest.Player) { p.Topology = p.RoomTopology("Renamed") })
	stale()
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	f.s.refresh(cancelled)
	if rooms := f.s.Rooms(f.ctx); len(rooms) != 1 || rooms[0].Name != "Renamed" {
		t.Fatalf("after a cancelled request: %+v", rooms)
	}
	// Nobody answering changes nothing: the room is unreachable, not unknown.
	f.p.Srv.Close()
	stale()
	f.s.refresh(f.ctx)
	if rooms := f.s.Rooms(f.ctx); len(rooms) != 1 {
		t.Fatalf("after nobody answered: %+v", rooms)
	}
	if _, err := f.s.Get(f.ctx, roomID); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Get: %v", err)
	}
}

// A speaker may fetch a song before answering the call that told it to.
func TestSpeakerFetchingBeforeAnswering(t *testing.T) {
	f := newFixture(t)
	codes := make(chan int, 2)
	f.p.Set(func(p *speakerstest.Player) {
		p.OnCall = func(action string, args map[string]string) {
			if action == "SetAVTransportURI" {
				codes <- f.fetch(pathOf(t, args["CurrentURI"])).Code
			}
		}
	})
	began := time.Now()
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:1], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	if code := <-codes; code != http.StatusOK {
		t.Fatalf("fetch: %d", code)
	}
	if d := time.Since(began); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestTopologyAddressesAreChecked(t *testing.T) {
	for addr, want := range map[string]bool{
		"10.1.1.169:1400":    true,
		"192.168.1.5:1400":   true,
		"172.16.0.9:1400":    true,
		"169.254.10.1:1400":  true,
		"10.1.1.169:8080":    false,
		"127.0.0.1:1400":     false,
		"203.0.113.9:1400":   false,
		"sonos.example:1400": false,
		"localhost:1400":     false,
		"10.1.1.169":         false,
		"224.0.0.1:1400":     false,
	} {
		if got := localPlayerAddr(addr); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
	// A room or a coordinator somewhere else doesn't exist; neither does a sneaky URL.
	rooms, addrs, err := parseTopology(`<ZoneGroupState><ZoneGroups>
		<ZoneGroup Coordinator="A"><ZoneGroupMember UUID="A" Location="http://10.0.0.1:1400/x" ZoneName="Good">
			<Satellite UUID="S" Location="http://127.0.0.1:32500/x" ZoneName="Good" Invisible="1"/></ZoneGroupMember></ZoneGroup>
		<ZoneGroup Coordinator="B"><ZoneGroupMember UUID="B" Location="http://127.0.0.1:1400/x" ZoneName="Loopback"/></ZoneGroup>
		<ZoneGroup Coordinator="C"><ZoneGroupMember UUID="C" Location="http://203.0.113.9:1400/x" ZoneName="Outside"/>
			<ZoneGroupMember UUID="D" Location="http://10.0.0.2:1400/x" ZoneName="Led from outside"/></ZoneGroup>
		<ZoneGroup Coordinator="E"><ZoneGroupMember UUID="E" Location="http://10.0.0.3:1400@evil.example/x" ZoneName="Userinfo"/></ZoneGroup>
		<ZoneGroup Coordinator="F"><ZoneGroupMember UUID="F" Location="ftp://10.0.0.4:1400/x" ZoneName="Scheme"/></ZoneGroup>
		</ZoneGroups></ZoneGroupState>`, localPlayerAddr)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Name != "Good" {
		t.Fatalf("%+v", rooms)
	}
	if _, ok := addrs["S"]; ok {
		t.Fatalf("%v", addrs)
	}
}

// A device can't take a known player's id while that player still answers; one that has really
// moved is believed.
func TestPlayerIDCantBeTakenOver(t *testing.T) {
	f := newFixture(t)
	f.get(t)
	settle := func() Room {
		t.Helper()
		rooms := f.s.settleAddrs(f.ctx, []Room{
			{ID: roomID, Name: "Room", Addr: "10.0.0.66:1400", Coordinator: roomID, CoordinatorAddr: "10.0.0.66:1400"},
			{ID: roomID, Name: "Twice", Addr: "10.0.0.67:1400", Coordinator: roomID, CoordinatorAddr: "10.0.0.67:1400"},
		}, map[string]string{roomID: "10.0.0.66:1400"})
		if len(rooms) != 1 {
			t.Fatalf("%+v", rooms)
		}
		return rooms[0]
	}
	if r := settle(); r.Addr != f.p.Addr || r.CoordinatorAddr != f.p.Addr {
		t.Fatalf("rebound while it answers: %+v", r)
	}
	f.p.Srv.Close()
	if r := settle(); r.Addr != "10.0.0.66:1400" {
		t.Fatalf("moved: %+v", r)
	}
}

// What's added by someone else is their play, not the owner's.
func TestPlaysCountForWhoeverAddedThem(t *testing.T) {
	f := newFixture(t)
	r, err := f.db.Exec(`INSERT INTO users (username, display_name) VALUES ('adult', 'A')`)
	if err != nil {
		t.Fatal(err)
	}
	adult, _ := r.LastInsertId()
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[:1], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	added := f.tracks[1]
	added.AddedBy = adult
	if _, err := f.s.Add(f.ctx, roomID, []Track{added}); err != nil {
		t.Fatal(err)
	}
	f.at("0:00:40", "0:01:00")
	f.get(t)
	f.at("0:00:59", "0:01:00")
	f.get(t)
	f.p.Finish()
	f.get(t)
	f.at("0:00:40", "0:01:00")
	f.get(t)
	var owner, theirs int64
	f.db.QueryRow(`SELECT item_id FROM play_history WHERE user_id = ?`, f.user).Scan(&owner)
	f.db.QueryRow(`SELECT item_id FROM play_history WHERE user_id = ?`, adult).Scan(&theirs)
	if owner != f.tracks[0].ItemID || theirs != added.ItemID {
		t.Fatalf("owner played %d, the adult %d", owner, theirs)
	}
	var plays int
	f.db.QueryRow(`SELECT COUNT(*) FROM user_item_state WHERE user_id = ? AND item_id = ? AND play_count > 0`, f.user, added.ItemID).Scan(&plays)
	if plays != 0 {
		t.Fatal("the owner's play count has someone else's song")
	}
}

func TestPauseWhileConverting(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	f.s.Convert = func(ctx context.Context, userID, itemID, fileID int64, sampleRate int, out string) error {
		<-release
		return os.WriteFile(out, []byte("converted"), 0o644)
	}
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[3:4], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	if st, err := f.s.Pause(f.ctx, roomID); err != nil || st.State != "paused" {
		t.Fatalf("Pause: %+v %v", st, err)
	}
	close(release)
	ss := f.s.session(roomID)
	<-ss.files[f.tracks[3].ItemID].done
	if st := f.get(t); st.State != "paused" || !st.Marquee {
		t.Fatalf("ready while paused: %+v", st)
	}
	if n := countOf(f.p.Actions(), "Play"); n != 0 {
		t.Fatalf("%d Play", n)
	}
	if st, err := f.s.Resume(f.ctx, roomID); err != nil || st.ItemID != f.tracks[3].ItemID {
		t.Fatalf("Resume: %+v %v", st, err)
	}
	if got := f.p.Get(); got.State != "PLAYING" || !strings.Contains(got.URI, ".m4a") {
		t.Fatalf("%+v", got)
	}
}

// With nothing of ours on the speaker, anything playing there is someone else's.
func TestTakeoverWhilePausedAndConverting(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	defer close(release)
	f.s.Convert = func(ctx context.Context, userID, itemID, fileID int64, sampleRate int, out string) error {
		<-release
		return ctx.Err()
	}
	if _, err := f.s.Play(f.ctx, roomID, f.user, f.tracks[3:4], 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Pause(f.ctx, roomID); err != nil {
		t.Fatal(err)
	}
	f.p.Set(func(p *speakerstest.Player) { p.State, p.URI = "PLAYING", "x-sonos-spotify:elsewhere" })
	if st := f.get(t); st.Marquee {
		t.Fatalf("%+v", st)
	}
}

// A hung room holds up its own plays, not another room's.
func TestHungRoomDoesntHoldUpOthers(t *testing.T) {
	f := newFixture(t)
	other := speakerstest.New(t, "RINCON_SECOND")
	f.s.Hosts = []string{f.p.Addr, other.Addr}
	f.p.Set(func(p *speakerstest.Player) {
		p.Topology = `<ZoneGroupState><ZoneGroups>
			<ZoneGroup Coordinator="` + roomID + `"><ZoneGroupMember UUID="` + roomID + `" Location="http://` + p.Addr + `/x" ZoneName="Hung"/></ZoneGroup>
			<ZoneGroup Coordinator="RINCON_SECOND"><ZoneGroupMember UUID="RINCON_SECOND" Location="http://` + other.Addr + `/x" ZoneName="Fine"/></ZoneGroup>
			</ZoneGroups></ZoneGroupState>`
	})
	if rooms := f.s.Rooms(f.ctx); len(rooms) != 2 {
		t.Fatalf("%+v", rooms)
	}
	hung := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	f.p.Set(func(p *speakerstest.Player) {
		p.OnCall = func(string, map[string]string) {
			select {
			case hung <- struct{}{}:
			default:
			}
			<-release
		}
	})
	go f.s.Play(f.ctx, roomID, f.user, f.tracks[:1], 0, 0, "")
	<-hung
	began := time.Now()
	st, err := f.s.Play(f.ctx, "RINCON_SECOND", f.user, f.tracks[:1], 0, 0, "")
	if err != nil || !st.Marquee {
		t.Fatalf("%+v %v", st, err)
	}
	if d := time.Since(began); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestStartPositionDoesntCarryOver(t *testing.T) {
	f := newFixture(t)
	f.s.Convert = func(ctx context.Context, userID, itemID, fileID int64, sampleRate int, out string) error {
		return errors.New("no")
	}
	if _, err := f.s.Play(f.ctx, roomID, f.user, []Track{f.tracks[3], f.tracks[1]}, 0, 20000, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { f.get(t); return f.p.Get().State == "PLAYING" })
	if got := f.p.Get(); !strings.Contains(got.URI, fmt.Sprintf("/%d.mp3", f.tracks[1].ItemID)) {
		t.Fatalf("%+v", got)
	}
	if n := countOf(f.p.Actions(), "Seek"); n != 0 {
		t.Fatalf("%d Seek into the next song", n)
	}
}
