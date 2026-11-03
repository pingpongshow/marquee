package intown

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/items"
	"marquee/internal/settings"
)

const testKey = "test-consumer-key"

// fakeTM answers like the Discovery API v2, from its documented response shape and what it was
// seen to send: quota headers on every response, attractions in no useful order, tribute bands
// included, and venues with no distance on a search by postcode.
type fakeTM struct {
	t *testing.T
	*httptest.Server
	mu          sync.Mutex
	attractions map[string][]map[string]any // by keyword
	shows       map[string][]map[string]any // by attractionId
	near        []map[string]any
	calls       []string
	status      int // answered instead, when set
	headers     map[string]string
	body        string
	available   int
	// failFor answers these searches with a server error; hold makes calls wait until closed.
	failFor map[string]bool
	hold    chan struct{}
}

func newFakeTM(t *testing.T) *fakeTM {
	f := &fakeTM{t: t, attractions: map[string][]map[string]any{}, shows: map[string][]map[string]any{}, available: 4000}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeTM) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hold := f.hold
	q := r.URL.Query()
	if hold != nil {
		f.calls = append(f.calls, "held")
		f.mu.Unlock()
		<-hold
		f.mu.Lock()
		f.calls = f.calls[:len(f.calls)-1]
	}
	defer f.mu.Unlock()
	if q.Get("apikey") != testKey {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"fault":{"faultstring":"Invalid ApiKey","detail":{"errorcode":"oauth.v2.InvalidApiKey"}}}`)
		return
	}
	f.calls = append(f.calls, r.URL.Path+" "+q.Get("keyword")+q.Get("attractionId")+q.Get("postalCode")+q.Get("city")+q.Get("geoPoint"))
	w.Header().Set("Rate-Limit", "5000")
	w.Header().Set("Rate-Limit-Available", fmt.Sprint(f.available))
	w.Header().Set("Rate-Limit-Over", "0")
	for k, v := range f.headers {
		w.Header().Set(k, v)
	}
	if f.failFor[q.Get("keyword")] {
		w.WriteHeader(500)
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		fmt.Fprint(w, f.body)
		return
	}
	switch r.URL.Path {
	case "/attractions.json":
		json.NewEncoder(w).Encode(map[string]any{"_embedded": map[string]any{"attractions": f.attractions[q.Get("keyword")]}, "page": map[string]any{"totalPages": 1}})
	case "/events.json":
		list := f.near
		if id := q.Get("attractionId"); id != "" {
			list = f.shows[id]
		}
		json.NewEncoder(w).Encode(map[string]any{"_embedded": map[string]any{"events": list}, "page": map[string]any{"totalPages": 1}})
	default:
		w.WriteHeader(404)
	}
}

func (f *fakeTM) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func tmAttr(id, name string, upcoming int, mbid string) map[string]any {
	a := map[string]any{"id": id, "name": name, "type": "attraction", "test": false, "upcomingEvents": map[string]any{"_total": upcoming},
		"classifications": []any{map[string]any{"primary": true, "segment": map[string]any{"name": "Music"}, "genre": map[string]any{"name": "Rock"}}}}
	if mbid != "" {
		a["externalLinks"] = map[string]any{"musicbrainz": []any{map[string]any{"id": mbid}}}
	}
	return a
}

// tmEventJSON is an event as the API sends it. Coordinates are strings; times are local and UTC.
func tmEventJSON(id, date, zone, venue, city string, lat, lon string, acts ...map[string]any) map[string]any {
	return map[string]any{"id": id, "name": "Show " + id, "type": "event", "test": false, "url": "https://www.ticketmaster.com/event/" + id,
		"dates": map[string]any{"start": map[string]any{"localDate": date, "localTime": "19:30:00", "dateTime": date + "T23:30:00Z", "dateTBD": false, "dateTBA": false, "timeTBA": false, "noSpecificTime": false},
			"timezone": zone, "status": map[string]any{"code": "onsale"}},
		"classifications": []any{map[string]any{"primary": true, "segment": map[string]any{"name": "Music"}, "genre": map[string]any{"name": "Rock"}, "subGenre": map[string]any{"name": "Alternative Rock"}}},
		"_embedded": map[string]any{
			"venues": []any{map[string]any{"name": venue, "postalCode": "94102", "timezone": zone, "city": map[string]any{"name": city}, "state": map[string]any{"name": "California", "stateCode": "CA"},
				"country": map[string]any{"name": "United States Of America", "countryCode": "US"}, "location": map[string]any{"latitude": lat, "longitude": lon}}},
			"attractions": acts,
		}}
}

// world is a main database, a service and a fake Ticketmaster.
type world struct {
	t    *testing.T
	db   *sql.DB
	svc  *Service
	tm   *fakeTM
	cfg  settings.InTown
	now  time.Time
	lib  int64
	lib2 int64
	user int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(dir, "m.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	w := &world{t: t, db: database, tm: newFakeTM(t), cfg: settings.InTown{Enabled: true, TicketmasterKey: testKey, DailyBudget: 2000},
		now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	w.svc = &Service{Path: filepath.Join(dir, "cache", "in-town.db"), Items: items.NewStore(database), Base: w.tm.URL,
		Settings: func() settings.InTown { return w.cfg }, Now: func() time.Time { return w.now }}
	w.svc.setup()
	w.svc.budget.Sleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { w.svc.closeCache(false) })
	w.lib = w.insert(`INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	w.lib2 = w.insert(`INSERT INTO libraries(name, type) VALUES ('Dad', 'music')`)
	w.user = w.insert(`INSERT INTO users(username, display_name, password_hash) VALUES ('u', 'U', 'x')`)
	return w
}

func (w *world) insert(q string, args ...any) int64 {
	w.t.Helper()
	r, err := w.db.Exec(q, args...)
	if err != nil {
		w.t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

func (w *world) artist(lib int64, name, mbid string) int64 {
	id := w.insert(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', ?, ?)`, lib, name, name)
	if mbid != "" {
		w.insert(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'musicbrainz', ?)`, id, mbid)
	}
	return id
}

func (w *world) track(artist int64, title string) int64 {
	var lib int64
	w.db.QueryRow(`SELECT library_id FROM items WHERE id = ?`, artist).Scan(&lib)
	return w.insert(`INSERT INTO items(library_id, type, title, sort_title, grandparent_id) VALUES (?, 'track', ?, ?, ?)`, lib, title, title, artist)
}

func (w *world) mark(item int64, col string, v any) {
	w.t.Helper()
	w.insert(`INSERT INTO user_item_state(user_id, item_id, `+col+`) VALUES (?, ?, ?) ON CONFLICT(user_id, item_id) DO UPDATE SET `+col+` = excluded.`+col, w.user, item, v)
}

func (w *world) input(acc items.Access, loc *Location) PageInput {
	w.t.Helper()
	ctx := context.Background()
	acc.UserID = w.user
	in := PageInput{Location: loc}
	var err error
	if in.Artists, err = w.svc.Items.MusicArtists(ctx); err != nil {
		w.t.Fatal(err)
	}
	if in.Visible, err = w.svc.Items.VisibleMusicArtists(ctx, acc); err != nil {
		w.t.Fatal(err)
	}
	if in.Liking, err = w.svc.Items.ArtistLikings(ctx, w.user, in.Visible); err != nil {
		w.t.Fatal(err)
	}
	return in
}

// ---------- matching ----------

func TestNormalizeName(t *testing.T) {
	for _, c := range [][2]string{
		{"Björk", "bjork"}, {"AC/DC", "ac dc"}, {"Simon & Garfunkel", "simon and garfunkel"}, {"Simon and Garfunkel", "simon and garfunkel"},
		{"Killers, The", "the killers"}, {"The Killers", "the killers"}, {"  Sigur   Rós ", "sigur ros"}, {"!!!", "!!!"}, {"Hüsker Dü", "husker du"},
	} {
		if got := normalizeName(c[0]); got != c[1] {
			t.Errorf("%q: got %q, want %q", c[0], got, c[1])
		}
	}
	if normalizeName("The Who") == normalizeName("Who") {
		t.Error(`"The Who" isn't "Who"`)
	}
	if normalizeName("M.I.A.") == normalizeName("Mia") {
		t.Error(`"M.I.A." isn't "Mia"`)
	}
	for _, same := range [][2]string{{"M.I.A.", "M.I.A"}, {"m.i.a.", "M.I.A."}, {"T. Rex", "T Rex"}, {"Dr. Dre", "Dr Dre"}, {"St. Vincent", "St Vincent"}} {
		if normalizeName(same[0]) != normalizeName(same[1]) {
			t.Errorf("%q and %q should match", same[0], same[1])
		}
	}
}

func TestMatchAttraction(t *testing.T) {
	const mbid, other = "a74b1b7f-71a5-4011-9441-d0b5e4122711", "11111111-2222-3333-4444-555555555555"
	music := func(id, name string, mbids ...string) Attraction {
		return Attraction{ID: id, Name: name, Music: true, MBIDs: mbids, Upcoming: -1}
	}
	tributes := []Attraction{music("t1", "Just Radiohead"), music("t2", "Karma Police - Radiohead tribute")}
	for _, c := range []struct {
		name   string
		artist Artist
		cands  []Attraction
		want   string
	}{
		{"exact name behind its tribute bands", Artist{Name: "Radiohead"}, append(tributes[:2:2], music("r", "Radiohead")), "r"},
		{"only tribute bands", Artist{Name: "Radiohead"}, tributes, ""},
		{"by MusicBrainz id, whatever the name", Artist{Name: "Radiohead (UK)", MBID: mbid}, []Attraction{music("r", "Radiohead", mbid)}, "r"},
		{"id wins over a same-named act", Artist{Name: "Nirvana", MBID: mbid}, []Attraction{music("uk", "Nirvana", other), music("us", "Nirvana", mbid)}, "us"},
		{"a different id is someone else", Artist{Name: "Nirvana", MBID: mbid}, []Attraction{music("uk", "Nirvana", other)}, ""},
		{"an id with a name-only act of that name", Artist{Name: "Radiohead", MBID: mbid}, []Attraction{music("r", "Radiohead")}, "r"},
		{"two of the name and no ids: no guess", Artist{Name: "Nirvana"}, []Attraction{music("a", "Nirvana"), music("b", "NIRVANA")}, ""},
		{"two of the name, one with an id, and an artist with none", Artist{Name: "Nirvana"}, []Attraction{music("a", "Nirvana", other), music("b", "Nirvana")}, ""},
		{"normalised", Artist{Name: "Killers, The"}, []Attraction{music("k", "The Killers")}, "k"},
		{"accents", Artist{Name: "Bjork"}, []Attraction{music("b", "Björk")}, "b"},
		{"similar isn't enough", Artist{Name: "The Band"}, []Attraction{music("b", "Band")}, ""},
		{"the library has two of the name", Artist{Name: "Nirvana", NameShared: true}, []Attraction{music("a", "Nirvana")}, ""},
		{"not music", Artist{Name: "Chicago"}, []Attraction{{ID: "c", Name: "Chicago", Music: false}}, ""},
	} {
		got, ok := matchAttraction(c.artist, c.cands)
		if ok != (c.want != "") || got.ID != c.want {
			t.Errorf("%s: got %q %v, want %q", c.name, got.ID, ok, c.want)
		}
	}
}

// ---------- the client ----------

func TestTicketmasterReadsDocumentedShape(t *testing.T) {
	const mbid = "a74b1b7f-71a5-4011-9441-d0b5e4122711"
	f := newFakeTM(t)
	f.attractions["Radiohead"] = []map[string]any{tmAttr("K1", "Radiohead", 3, mbid)}
	ok := tmEventJSON("E1", "2026-11-20", "America/Los_Angeles", "August Hall", "San Francisco", "37.787", "-122.409", tmAttr("K1", "Radiohead", 3, mbid), tmAttr("K2", "Support", 0, ""))
	cancelled := tmEventJSON("E2", "2026-11-21", "UTC", "V", "C", "1", "1")
	cancelled["dates"].(map[string]any)["status"] = map[string]any{"code": "cancelled"}
	badLink := tmEventJSON("E3", "2026-11-22", "UTC", "V", "C", "1", "1")
	badLink["url"] = "javascript:alert(1)"
	test := tmEventJSON("E4", "2026-11-23", "UTC", "V", "C", "1", "1")
	test["test"] = true
	noDate := tmEventJSON("E5", "", "UTC", "V", "C", "1", "1")
	noTime := tmEventJSON("E6", "2026-11-24", "", "V", "C", "1", "1")
	noTime["dates"].(map[string]any)["start"].(map[string]any)["noSpecificTime"] = true
	postponed := tmEventJSON("E7", "2026-11-25", "UTC", "V", "C", "1", "1")
	postponed["dates"].(map[string]any)["status"] = map[string]any{"code": "postponed"}
	offsale := tmEventJSON("E8", "2026-11-26", "UTC", "V", "C", "1", "1")
	offsale["dates"].(map[string]any)["status"] = map[string]any{"code": "offsale"}
	f.shows["K1"] = []map[string]any{ok, cancelled, badLink, test, noDate, noTime, postponed, offsale}

	tm := &Ticketmaster{Key: func() string { return testKey }, Base: f.URL}
	found, err := tm.Attractions(context.Background(), "Radiohead")
	if err != nil || len(found) != 1 {
		t.Fatalf("attractions: %v %+v", err, found)
	}
	if a := found[0]; a.ID != "K1" || !a.Music || a.Genre != "Rock" || a.Upcoming != 3 || !slices.Equal(a.MBIDs, []string{mbid}) {
		t.Errorf("attraction: %+v", a)
	}
	shows, err := tm.ArtistShows(context.Background(), "K1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range shows {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{"E1", "E6", "E7", "E8"}) {
		t.Fatalf("kept %v", ids)
	}
	s := shows[0]
	if s.LocalDate != "2026-11-20" || s.LocalTime != "19:30" || s.TimeZone != "America/Los_Angeles" || s.StartsAt.Format(time.RFC3339) != "2026-11-20T23:30:00Z" {
		t.Errorf("times: %+v", s)
	}
	if !s.Venue.HasPoint || s.Venue.Lat != 37.787 || s.Venue.City != "San Francisco" || s.Venue.Region != "CA" || s.Venue.Country != "US" {
		t.Errorf("venue: %+v", s.Venue)
	}
	if len(s.Acts) != 2 || s.Acts[1].Name != "Support" || s.Status != "scheduled" || s.SubGenre != "Alternative Rock" {
		t.Errorf("show: %+v", s)
	}
	if shows[1].LocalTime != "" || !shows[1].StartsAt.IsZero() {
		t.Errorf("no specific time: %+v", shows[1])
	}
	if shows[2].Status != "postponed" {
		t.Errorf("postponed: %+v", shows[2])
	}
}

// The key is in every request's URL, so it must not get out through what goes wrong.
func TestKeyStaysOutOfErrorsAndLogs(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	w := newWorld(t)
	w.artist(w.lib, "Radiohead", "")
	w.tm.status = 500
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := w.svc.Status(context.Background())
	if st.LastError == "" {
		t.Error("a server error should show in the status")
	}
	// Nothing listening at all: the transport's error names the URL.
	w.tm.Close()
	w.now = w.now.Add(artistRetry + time.Minute)
	msg, err := w.svc.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st = w.svc.Status(context.Background())
	if !strings.Contains(st.LastError, "couldn't reach Ticketmaster") {
		t.Errorf("status: %q", st.LastError)
	}
	for name, text := range map[string]string{"logs": logs.String(), "status": st.LastError, "message": msg} {
		if strings.Contains(text, testKey) {
			t.Errorf("the key is in the %s: %s", name, text)
		}
		if strings.Contains(strings.ToLower(text), "apikey=") {
			t.Errorf("the key's parameter is in the %s: %s", name, text)
		}
	}
	// And the second line of defence: anything with it in gets it taken out.
	if got := w.svc.redact("GET https://x/events.json?apikey=" + testKey + "&size=1: " + testKey); strings.Contains(got, testKey) {
		t.Errorf("redact: %q", got)
	}
}

func TestBadKeyStopsAndSays(t *testing.T) {
	w := newWorld(t)
	for _, n := range []string{"A", "B", "C"} {
		w.artist(w.lib, n, "")
	}
	w.cfg.TicketmasterKey = "wrong"
	msg, err := w.svc.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "rejected the key") {
		t.Errorf("message: %q", msg)
	}
	st := w.svc.Status(context.Background())
	if !strings.Contains(st.LastError, "rejected the key") {
		t.Errorf("status: %q", st.LastError)
	}
	if strings.Contains(st.LastError, "wrong") {
		t.Errorf("the key is in the status: %q", st.LastError)
	}
}

// ---------- budget ----------

func TestBudgetPaceAndAllowance(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	daily := 3
	b := newBudget(func() int { return daily }, func() time.Time { return now })
	var waits []time.Duration
	b.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	bg := background(context.Background())
	for range 3 {
		if err := b.Before(bg); err != nil {
			t.Fatal(err)
		}
	}
	// At most two calls a second.
	if !slices.Equal(waits, []time.Duration{500 * time.Millisecond, time.Second}) {
		t.Errorf("waits %v", waits)
	}
	if err := b.Before(bg); !errors.Is(err, ErrOverBudget) {
		t.Errorf("background past its budget: %v", err)
	}
	// People's requests have the room that was left for them.
	if err := b.Before(context.Background()); err != nil {
		t.Errorf("a person's request: %v", err)
	}
	// A new day starts afresh.
	now = now.Add(24 * time.Hour)
	if err := b.Before(bg); err != nil {
		t.Errorf("next day: %v", err)
	}
	// The source's own count: background leaves the last of it alone, people may use it.
	b.After(Quota{Limit: 5000, Available: backgroundReserve, Reset: now.Add(time.Hour)})
	if err := b.Before(bg); !errors.Is(err, ErrOverBudget) {
		t.Errorf("background into the reserve: %v", err)
	}
	if err := b.Before(context.Background()); err != nil {
		t.Errorf("a person into the reserve: %v", err)
	}
	// Past the hard cap, nobody calls.
	b.state.Used = hardDailyCap
	if err := b.Before(context.Background()); !errors.Is(err, ErrOverBudget) {
		t.Errorf("past the cap: %v", err)
	}
}

func TestBudgetBackoff(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	b := newBudget(func() int { return 100 }, func() time.Time { return now })
	b.Sleep = func(context.Context, time.Duration) error { return nil }
	b.Limited(now.Add(2 * time.Hour))
	if err := b.Before(context.Background()); !errors.Is(err, ErrOverBudget) {
		t.Errorf("during a wait: %v", err)
	}
	now = now.Add(2*time.Hour + time.Second)
	if err := b.Before(context.Background()); err != nil {
		t.Errorf("after a wait: %v", err)
	}
	// No time given: waits grow, to a limit.
	var got []time.Duration
	for range 7 {
		b.Limited(time.Time{})
		got = append(got, b.state.Backoff.Sub(now))
	}
	if got[0] != firstBackoff || got[1] != 2*firstBackoff || got[6] != maxBackoff {
		t.Errorf("waits %v", got)
	}
}

// A 429 stops the job and holds further calls until the reset it names, in milliseconds.
func TestSyncStopsAt429AndHonoursReset(t *testing.T) {
	w := newWorld(t)
	for _, n := range []string{"A", "B", "C"} {
		w.artist(w.lib, n, "")
	}
	reset := w.now.Add(3 * time.Hour)
	w.tm.status = 429
	w.tm.body = `{"fault":{"faultstring":"Rate limit quota violation","detail":{"errorcode":"policies.ratelimit.QuotaViolation"}}}`
	w.tm.headers = map[string]string{"Rate-Limit-Reset": fmt.Sprint(reset.UnixMilli())}
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := w.tm.callCount(); n != 1 {
		t.Errorf("%d calls after a 429", n)
	}
	st := w.svc.Status(context.Background())
	if !st.Backoff.Equal(reset.Truncate(time.Millisecond)) {
		t.Errorf("backoff %v, want %v", st.Backoff, reset)
	}
	w.tm.status = 0
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := w.tm.callCount(); n != 1 {
		t.Errorf("called during the wait: %d", n)
	}
	w.now = reset.Add(time.Minute)
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := w.tm.callCount(); n != 4 {
		t.Errorf("after the wait: %d calls", n)
	}
}

// The per-second limit shares the status, but its reset header names the end of the day.
func TestSpikeArrestIsAShortWait(t *testing.T) {
	w := newWorld(t)
	w.artist(w.lib, "A", "")
	w.tm.status = 429
	w.tm.body = `{"fault":{"detail":{"errorcode":"policies.ratelimit.SpikeArrestViolation"}}}`
	w.tm.headers = map[string]string{"Rate-Limit-Reset": fmt.Sprint(time.Now().Add(20 * time.Hour).UnixMilli())}
	w.now = time.Now()
	w.svc.Sync(context.Background())
	if st := w.svc.Status(context.Background()); st.Backoff.After(time.Now().Add(2 * spikeWait)) {
		t.Errorf("a spike held calls until %v", st.Backoff)
	}
}

// ---------- the job ----------

func TestSyncMatchesFetchesAndSkipsWhatItCan(t *testing.T) {
	const mbid = "a74b1b7f-71a5-4011-9441-d0b5e4122711"
	w := newWorld(t)
	w.artist(w.lib, "Radiohead", mbid)
	w.artist(w.lib, "Quiet Band", "")
	w.tm.attractions["Radiohead"] = []map[string]any{tmAttr("T", "Just Radiohead", 17, ""), tmAttr("R", "Radiohead", 1, mbid)}
	w.tm.attractions["Quiet Band"] = []map[string]any{tmAttr("Q", "Quiet Band", 0, "")}
	w.tm.shows["R"] = []map[string]any{tmEventJSON("E1", "2026-12-01", "Europe/London", "O2", "London", "51.5", "0", tmAttr("R", "Radiohead", 1, mbid))}
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A count of no upcoming shows saves asking for them.
	if slices.Contains(w.tm.calls, "/events.json Q") {
		t.Errorf("asked for shows of an act with none: %v", w.tm.calls)
	}
	shows, _, err := w.svc.ArtistShows(context.Background(), "Radiohead", mbid, nil)
	if err != nil || len(shows) != 1 || shows[0].ID != "E1" {
		t.Fatalf("shows: %v %+v", err, shows)
	}
	// Nothing's due an hour later, so no calls.
	before := w.tm.callCount()
	w.now = w.now.Add(time.Hour)
	w.svc.Sync(context.Background())
	if n := w.tm.callCount(); n != before {
		t.Errorf("%d calls with nothing due", n-before)
	}
	// A day later, shows are due again but the match isn't.
	w.now = w.now.Add(24 * time.Hour)
	w.svc.Sync(context.Background())
	got := w.tm.calls[before:]
	if !slices.Contains(got, "/events.json R") || slices.Contains(got, "/attractions.json Radiohead") {
		t.Errorf("a day later: %v", got)
	}
}

func TestSyncTakesArtistsPeopleCareAboutFirst(t *testing.T) {
	w := newWorld(t)
	w.cfg.DailyBudget = 3
	w.artist(w.lib, "Aaa Nobody", "")
	w.artist(w.lib, "Bbb Nobody", "")
	played := w.artist(w.lib, "Played", "")
	w.mark(w.track(played, "s"), "play_count", 5)
	rated := w.artist(w.lib, "Rated", "")
	w.mark(rated, "rating", 8.0)
	w.mark(w.artist(w.lib, "Hearted", ""), "favorite_at", "2026-01-01T00:00:00Z")
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range w.tm.calls {
		names = append(names, strings.TrimPrefix(c, "/attractions.json "))
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"Hearted", "Played", "Rated"}) {
		t.Errorf("looked up %v", names)
	}
}

func TestOpenedArtistComesFirstAndRefreshesWhenStale(t *testing.T) {
	w := newWorld(t)
	w.tm.attractions["Opened"] = []map[string]any{tmAttr("O", "Opened", 1, "")}
	w.tm.shows["O"] = []map[string]any{tmEventJSON("E1", "2026-12-01", "UTC", "V", "C", "1", "1", tmAttr("O", "Opened", 1, ""))}
	w.artist(w.lib, "Opened", "")
	w.svc.RefreshArtist(context.Background(), "Opened", "")
	if n := w.tm.callCount(); n != 2 {
		t.Errorf("%d calls refreshing a stale artist", n)
	}
	w.svc.RefreshArtist(context.Background(), "Opened", "")
	if n := w.tm.callCount(); n != 2 {
		t.Errorf("refreshed again while fresh: %d", n)
	}
	m := map[string]matchRow{"a": {Opened: w.now.Add(-time.Hour)}, "b": {}}
	due := dueWork(map[string]*artistGroup{"a": {Key: "a"}, "b": {Key: "b", Cared: true}}, m, nil, w.now)
	if due[0].Key != "a" {
		t.Errorf("order: %s first", due[0].Key)
	}
}

// ---------- past shows ----------

func TestIsPastGoesByTheVenuesDate(t *testing.T) {
	// 05:00 UTC on the 3rd is still the evening of the 2nd in Los Angeles, and already the
	// afternoon of the 3rd in Auckland.
	now := time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		date, zone string
		grace      int
		want       bool
	}{
		{"2026-11-02", "America/Los_Angeles", 0, false}, // tonight there
		{"2026-11-02", "Pacific/Auckland", 0, true},     // over there
		{"2026-11-03", "Pacific/Auckland", 0, false},    // tonight there
		{"2026-11-01", "America/Los_Angeles", 0, true},
		{"2026-11-02", "Pacific/Auckland", 1, false}, // a day's grace
		{"2026-11-01", "Pacific/Auckland", 1, true},
		{"2026-11-02", "", 0, false}, // no zone: the last place on that date has it still
		{"2026-11-02", "Not/AZone", 0, false},
		{"2026-11-01", "", 0, true},
		{"not a date", "", 0, true},
	} {
		if got := isPast(c.date, c.zone, now, c.grace); got != c.want {
			t.Errorf("%s in %q, grace %d: got %v", c.date, c.zone, c.grace, got)
		}
	}
}

func TestPruneAndReadsDropPastShows(t *testing.T) {
	w := newWorld(t)
	w.tm.attractions["A"] = []map[string]any{tmAttr("A", "A", 3, "")}
	w.tm.shows["A"] = []map[string]any{
		tmEventJSON("old", "2026-10-30", "America/Los_Angeles", "V", "C", "1", "1", tmAttr("A", "A", 3, "")),
		tmEventJSON("tonightLA", "2026-11-02", "America/Los_Angeles", "V", "C", "1", "1", tmAttr("A", "A", 3, "")),
		tmEventJSON("later", "2026-12-02", "America/Los_Angeles", "V", "C", "1", "1", tmAttr("A", "A", 3, "")),
	}
	w.artist(w.lib, "A", "")
	w.now = time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC) // the evening of the 2nd in Los Angeles
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := w.svc.cur.Load()
	ids := func() []string {
		rows, err := c.db.Query(`SELECT id FROM shows ORDER BY local_date`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			out = append(out, id)
		}
		return out
	}
	if msg, err := w.svc.Prune(context.Background()); err != nil || msg != "Removed 1 past shows" {
		t.Errorf("prune: %q %v", msg, err)
	}
	if got := ids(); !slices.Equal(got, []string{"tonightLA", "later"}) {
		t.Errorf("after prune: %v", got)
	}
	// Two days on: the date has passed but the grace hasn't run out, so it's in the file and
	// out of what's read.
	w.now = time.Date(2026, 11, 4, 5, 0, 0, 0, time.UTC)
	w.svc.Prune(context.Background())
	if got := ids(); !slices.Contains(got, "tonightLA") {
		t.Errorf("grace: %v", got)
	}
	shows, _, _ := w.svc.ArtistShows(context.Background(), "A", "", nil)
	if len(shows) != 1 || shows[0].ID != "later" {
		t.Errorf("read a past show: %+v", shows)
	}
	w.now = time.Date(2026, 11, 5, 5, 0, 0, 0, time.UTC)
	w.svc.Prune(context.Background())
	if got := ids(); !slices.Equal(got, []string{"later"}) {
		t.Errorf("after the grace: %v", got)
	}
}

// ---------- the cache file ----------

func TestCacheStartsAfreshWhenCorruptOrAnotherVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sub", "c.db")
	c, err := openCache(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	c.setMeta(ctx, "k", "v")
	c.close()
	// Kept across opens.
	if c, err = openCache(ctx, path); err != nil || c.meta(ctx, "k") != "v" {
		t.Fatalf("reopen: %v", err)
	}
	// Another schema version.
	c.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion+1))
	c.close()
	if c, err = openCache(ctx, path); err != nil || c.meta(ctx, "k") != "" {
		t.Fatalf("other version kept: %v %q", err, c.meta(ctx, "k"))
	}
	var v int
	c.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != schemaVersion {
		t.Errorf("version %d", v)
	}
	c.close()
	// Not a database at all.
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")
	if err := os.WriteFile(path, bytes.Repeat([]byte("garbage!"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err = openCache(ctx, path); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	defer c.close()
	if _, err := c.matches(ctx); err != nil {
		t.Errorf("rebuilt: %v", err)
	}
}

func TestDamageWhileRunningStartsAfresh(t *testing.T) {
	w := newWorld(t)
	w.artist(w.lib, "A", "")
	c, err := w.svc.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.db.Exec(`DROP TABLE artist_matches`)
	if _, _, err := w.svc.ArtistShows(context.Background(), "A", "", nil); err == nil {
		t.Fatal("expected the first read to fail")
	}
	if _, _, err := w.svc.ArtistShows(context.Background(), "A", "", nil); err != nil {
		t.Errorf("the next should work on a new file: %v", err)
	}
}

func TestTurningOffDeletesTheFile(t *testing.T) {
	w := newWorld(t)
	w.artist(w.lib, "A", "")
	w.svc.Sync(context.Background())
	if _, err := os.Stat(w.svc.Path); err != nil {
		t.Fatal(err)
	}
	w.cfg.Enabled = false
	w.svc.Changed(w.cfg, false)
	for _, p := range []string{w.svc.Path, w.svc.Path + "-wal", w.svc.Path + "-shm"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if msg, _ := w.svc.Sync(context.Background()); msg != "In Town is off" {
		t.Errorf("sync while off: %q", msg)
	}
	if _, err := os.Stat(w.svc.Path); !errors.Is(err, os.ErrNotExist) {
		t.Error("the file came back")
	}
	// Started while off, a leftover file goes too.
	os.MkdirAll(filepath.Dir(w.svc.Path), 0o755)
	os.WriteFile(w.svc.Path, []byte("x"), 0o644)
	w.svc.Start(context.Background())
	if _, err := os.Stat(w.svc.Path); !errors.Is(err, os.ErrNotExist) {
		t.Error("left over after a start while off")
	}
}

// ---------- ranking ----------

func TestDislikes(t *testing.T) {
	for _, c := range []struct {
		name string
		l    items.ArtistLiking
		want bool
	}{
		{"nothing known", items.ArtistLiking{}, false},
		{"under 2 stars", items.ArtistLiking{Rating: 3}, true},
		{"exactly 2 stars", items.ArtistLiking{Rating: 4}, false},
		{"their music rated low", items.ArtistLiking{RatedParts: 2, RatedSum: 5}, true},
		{"one low rating is one song", items.ArtistLiking{RatedParts: 1, RatedSum: 2}, false},
		{"mostly thumbs down", items.ArtistLiking{Thumbs: 3, Touched: 4, Songs: 10}, true},
		{"half isn't mostly", items.ArtistLiking{Thumbs: 2, Touched: 4, Songs: 10}, false},
		{"one thumb of many", items.ArtistLiking{Thumbs: 1, Touched: 1, Songs: 10}, false},
		{"their only song", items.ArtistLiking{Thumbs: 1, Touched: 1, Songs: 1}, true},
		{"a heart wins", items.ArtistLiking{Favorite: true, Rating: 3, Thumbs: 3, Touched: 3}, false},
	} {
		if got := dislikes(c.l); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestRankNearOrderAndReasons(t *testing.T) {
	const (
		hearted = iota + 1
		rated
		played
		similar
		soundAlike
		inLibrary
		hated
		liked // never on a bill: what similar is like
	)
	view := func(id, act, date string, genre string) ShowView {
		return ShowView{Show: Show{ID: id, LocalDate: date, Genre: genre}, Act: act}
	}
	cand := func(id, date, genre string, refs ...actRef) nearCandidate {
		name := id
		if len(refs) > 0 {
			name = refs[0].Act.Name
		}
		return nearCandidate{View: view(id, name, date, genre), Acts: refs}
	}
	ref := func(name string, item int64) actRef { return actRef{Act: Attraction{Name: name}, ItemID: item} }
	taste := Taste{
		Liking: map[int64]items.ArtistLiking{hearted: {Favorite: true}, rated: {Rating: 8}, played: {Plays: 30}, similar: {}, soundAlike: {}, inLibrary: {},
			hated: {Rating: 2}, liked: {Rating: 10}},
		Names: map[int64]string{hearted: "Hearted", rated: "Rated", played: "Played", similar: "Similar", soundAlike: "Alike", inLibrary: "Library", hated: "Hated", liked: "Radiohead"},
		Similar: func(a int64) map[int64]float64 {
			if a == liked {
				return map[int64]float64{similar: 1}
			}
			return nil
		},
		SoundsLike: map[int64]int64{soundAlike: liked},
		Genres:     []string{"rock"},
	}
	out := rankNear([]nearCandidate{
		cand("nearby", "2026-11-03", "Polka", ref("Unknown", 0)),
		cand("genre", "2026-11-04", "Alternative Rock", ref("New Band", 0)),
		cand("lib", "2026-11-05", "", ref("Library", inLibrary)),
		cand("alike", "2026-11-06", "", ref("Alike", soundAlike)),
		cand("sim", "2026-11-07", "", ref("Similar", similar)),
		cand("plays", "2026-11-08", "", ref("Played", played)),
		cand("rate", "2026-11-09", "", ref("Rated", rated)),
		cand("heart", "2026-11-10", "", ref("Hearted", hearted)),
		cand("hatedHead", "2026-11-01", "Rock", ref("Hated", hated), ref("Rated", rated)),
		cand("hatedSupport", "2026-11-02", "", ref("Rated", rated), ref("Hated", hated)),
	}, taste)
	var got []string
	reasons := map[string]string{}
	for _, v := range out {
		got = append(got, v.ID)
		reasons[v.ID] = v.Reason
	}
	want := []string{"heart", "hatedSupport", "rate", "plays", "sim", "alike", "lib", "genre", "nearby"}
	if !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	for id, r := range map[string]string{"heart": "One of your favourites", "rate": "You rated them highly", "plays": "You play them a lot",
		"sim": "Like Radiohead", "alike": "Sounds like Radiohead", "lib": "In your library", "genre": "You listen to a lot of Alternative Rock", "nearby": "Near you"} {
		if reasons[id] != r {
			t.Errorf("%s: %q, want %q", id, reasons[id], r)
		}
	}
}

// A support act you like is who the entry is about, with the headliner still on the bill.
func TestSupportActCanLeadAShow(t *testing.T) {
	const fav = 1
	out := rankNear([]nearCandidate{{View: ShowView{Show: Show{ID: "x", LocalDate: "2026-11-03"}, Act: "Big Name"},
		Acts: []actRef{{Act: Attraction{Name: "Big Name"}}, {Act: Attraction{Name: "Opener"}, ItemID: fav}}}},
		Taste{Liking: map[int64]items.ArtistLiking{fav: {Favorite: true}}, Names: map[int64]string{fav: "Opener"}})
	if len(out) != 1 || out[0].Act != "Opener" || !out[0].Support || out[0].ItemID != fav {
		t.Errorf("%+v", out)
	}
}

func TestGenreMatchIsWholeWords(t *testing.T) {
	top := []string{"rock", "hip hop"}
	for _, c := range []struct {
		genre, sub, want string
	}{
		{"Rock", "", "Rock"}, {"Rockabilly", "", ""}, {"Alternative Rock", "", "Alternative Rock"}, {"Hip-Hop/Rap", "", "Hip-Hop/Rap"}, {"", "", ""}, {"Other", "Rock", "Rock"},
	} {
		if g, _ := genreMatch(Show{Genre: c.genre, SubGenre: c.sub}, top); g != c.want {
			t.Errorf("%q: %q", c.genre, g)
		}
	}
}

// ---------- the page ----------

func TestPageUpperAreaKeepsToTheLibrariesTheyCanSee(t *testing.T) {
	w := newWorld(t)
	w.artist(w.lib, "Mine", "")
	w.artist(w.lib2, "Dads", "")
	for _, n := range []string{"Mine", "Dads"} {
		w.tm.attractions[n] = []map[string]any{tmAttr(n, n, 1, "")}
		w.tm.shows[n] = []map[string]any{tmEventJSON(n+"1", "2026-12-01", "UTC", "V", "C", "1", "1", tmAttr(n, n, 1, ""))}
	}
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	names := func(acc items.Access) []string {
		page, err := w.svc.BuildPage(context.Background(), w.input(acc, nil))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, a := range page.Artists {
			out = append(out, a.Shows[0].Act)
		}
		slices.Sort(out)
		return out
	}
	if got := names(items.Access{}); !slices.Equal(got, []string{"Dads", "Mine"}) {
		t.Errorf("everything: %v", got)
	}
	if got := names(items.Access{LibraryIDs: []int64{w.lib}}); !slices.Equal(got, []string{"Mine"}) {
		t.Errorf("one library: %v", got)
	}
	if got := names(items.Access{LibraryIDs: []int64{}}); len(got) != 0 {
		t.Errorf("no libraries: %v", got)
	}
}

// An act in a library they can't see is just another act near them: no link, no reason from it.
func TestPageNearDoesntLeakHiddenArtists(t *testing.T) {
	w := newWorld(t)
	dads := w.artist(w.lib2, "Dads", "")
	w.mark(dads, "favorite_at", "2026-01-01T00:00:00Z")
	w.tm.near = []map[string]any{tmEventJSON("N1", "2026-12-01", "America/Los_Angeles", "August Hall", "San Francisco", "37.78", "-122.40", tmAttr("D", "Dads", 1, ""))}
	loc := &Location{PostalCode: "94102", CountryCode: "US", Distance: 50, Unit: "miles"}
	page, err := w.svc.BuildPage(context.Background(), w.input(items.Access{LibraryIDs: []int64{w.lib}}, loc))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Near) != 1 || page.Near[0].ItemID != 0 || page.Near[0].ReasonKind != ReasonNearby {
		t.Errorf("near: %+v", page.Near)
	}
}

func TestPageOrdersArtistsByLikingThenDateAndLeavesOutDislikes(t *testing.T) {
	w := newWorld(t)
	w.artist(w.lib, "Soon", "")
	w.artist(w.lib, "Later", "")
	hearted := w.artist(w.lib, "Hearted", "")
	rated := w.artist(w.lib, "Rated", "")
	hated := w.artist(w.lib, "Hated", "")
	thumbs := w.artist(w.lib, "Thumbs", "")
	w.mark(hearted, "favorite_at", "2026-01-01T00:00:00Z")
	w.mark(rated, "rating", 7.0)
	w.mark(hated, "rating", 3.0)
	for i := range 3 {
		w.mark(w.track(thumbs, fmt.Sprint("t", i)), "disliked_at", "2026-01-01T00:00:00Z")
	}
	dates := map[string]string{"Soon": "2026-11-10", "Later": "2026-11-20", "Hearted": "2027-01-01", "Rated": "2026-12-25", "Hated": "2026-11-05", "Thumbs": "2026-11-06"}
	for name, date := range dates {
		w.tm.attractions[name] = []map[string]any{tmAttr(name, name, 4, "")}
		var list []map[string]any
		for i := range 5 {
			d, _ := time.Parse(time.DateOnly, date)
			list = append(list, tmEventJSON(fmt.Sprint(name, i), d.AddDate(0, 0, i).Format(time.DateOnly), "UTC", "V", "C", "1", "1", tmAttr(name, name, 4, "")))
		}
		w.tm.shows[name] = list
	}
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err := w.svc.BuildPage(context.Background(), w.input(items.Access{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range page.Artists {
		got = append(got, a.Shows[0].Act)
	}
	if !slices.Equal(got, []string{"Hearted", "Rated", "Soon", "Later"}) {
		t.Errorf("order %v", got)
	}
	first := page.Artists[0]
	if first.ShowCount != 5 || len(first.Shows) != nextShows || first.Shows[0].LocalDate != "2027-01-01" || first.Reason != "One of your favourites" {
		t.Errorf("first: %+v", first)
	}
	if page.Artists[2].Reason != "" {
		t.Errorf("no reason for an artist with no signal: %q", page.Artists[2].Reason)
	}
}

func TestPageNearByLocationDistanceAndSupportAct(t *testing.T) {
	w := newWorld(t)
	opener := w.artist(w.lib, "Opener", "")
	w.mark(opener, "favorite_at", "2026-01-01T00:00:00Z")
	head, sup := tmAttr("H", "Big Name", 1, ""), tmAttr("O", "Opener", 1, "")
	w.tm.near = []map[string]any{
		tmEventJSON("close", "2026-12-01", "America/Los_Angeles", "August Hall", "San Francisco", "37.78", "-122.40", head, sup),
		tmEventJSON("close2", "2026-12-03", "America/Los_Angeles", "Fillmore", "San Francisco", "37.78", "-122.43", tmAttr("C", "Other Band", 1, "")),
		tmEventJSON("far", "2026-12-02", "America/Los_Angeles", "Arena", "Sacramento", "38.58", "-121.49", tmAttr("F", "Far Band", 1, "")),
	}
	loc := &Location{PostalCode: "94102", CountryCode: "US", Distance: 50, Unit: "miles"}
	page, err := w.svc.BuildPage(context.Background(), w.input(items.Access{}, loc))
	if err != nil {
		t.Fatal(err)
	}
	if !page.LocationFound || len(page.Near) != 2 {
		t.Fatalf("near: %+v", page)
	}
	v := page.Near[0]
	if v.ID != "close" || v.Act != "Opener" || !v.Support || v.ItemID != opener || !v.HasDistance || !v.Near {
		t.Errorf("show: %+v", v)
	}
	// Cached: another look doesn't ask again.
	calls := w.tm.callCount()
	if _, err := w.svc.BuildPage(context.Background(), w.input(items.Access{}, loc)); err != nil {
		t.Fatal(err)
	}
	if n := w.tm.callCount(); n != calls {
		t.Errorf("%d more calls while cached", n-calls)
	}
	// Someone nearby with a similar distance shares the search; hours later it's asked again.
	w.now = w.now.Add(nearTTL + time.Minute)
	w.svc.BuildPage(context.Background(), w.input(items.Access{}, loc))
	if n := w.tm.callCount(); n != calls+1 {
		t.Errorf("after it went stale: %d more calls", n-calls)
	}
	// A failure shows what was found, with a message.
	w.now = w.now.Add(nearTTL + time.Minute)
	w.tm.status = 500
	page, err = w.svc.BuildPage(context.Background(), w.input(items.Access{}, loc))
	if err != nil || page.NearError == "" || len(page.Near) != 2 {
		t.Errorf("on failure: %v %+v", err, page)
	}
}

func TestLocationNotFoundFallsBackToTheSearch(t *testing.T) {
	w := newWorld(t)
	loc := &Location{PostalCode: "ZZ1", CountryCode: "GB", Distance: 50, Unit: "miles"}
	page, err := w.svc.BuildPage(context.Background(), w.input(items.Access{}, loc))
	if err != nil {
		t.Fatal(err)
	}
	if page.LocationFound {
		t.Error("found a place with no venues")
	}
	// Close by, then wider, then the search itself.
	if want := []string{"/events.json ZZ1", "/events.json ZZ1", "/events.json ZZ1"}; !slices.Equal(w.tm.calls, want) {
		t.Errorf("calls %q", w.tm.calls)
	}
}

func TestResolverRefusesWhatALookupDidnt(t *testing.T) {
	const mbid = "a74b1b7f-71a5-4011-9441-d0b5e4122711"
	groups := groupArtists([]items.MusicArtist{{ID: 1, Name: "Unique"}, {ID: 2, Name: "Refused"}, {ID: 3, Name: "With Id", MBID: mbid}, {ID: 4, Name: "Twin"}, {ID: 5, Name: "Twin", MBID: mbid + "x"}, {ID: 6, Name: "Twice"}})
	r := newResolver(groups, map[string]matchRow{"name:refused": {Key: "name:refused", CheckedAt: time.Now()}}, map[string]int{"twice": 2})
	for _, c := range []struct {
		a    Attraction
		want string
	}{
		{Attraction{ID: "1", Name: "Unique"}, "name:unique"},
		{Attraction{ID: "2", Name: "Refused"}, ""},
		{Attraction{ID: "3", Name: "Anything", MBIDs: []string{mbid}}, "mbid:" + mbid},
		{Attraction{ID: "4", Name: "With Id", MBIDs: []string{"other"}}, ""},
		{Attraction{ID: "5", Name: "Twin"}, ""},
		{Attraction{ID: "6", Name: "Twice"}, ""},
		{Attraction{ID: "7", Name: "Stranger"}, ""},
	} {
		key, ok := r.key(c.a)
		if ok != (c.want != "") || key != c.want {
			t.Errorf("%s: %q %v, want %q", c.a.Name, key, ok, c.want)
		}
	}
}

func TestClean(t *testing.T) {
	got, err := Clean(Location{PostalCode: " sw1a  1aa ", CountryCode: "gb"})
	if err != nil || got.PostalCode != "SW1A" || got.CountryCode != "GB" || got.Distance != 50 || got.Unit != "miles" {
		t.Errorf("%+v %v", got, err)
	}
	got, err = Clean(Location{City: " Berlin ", CountryCode: "DE"})
	if err != nil || got.City != "Berlin" || got.Distance != 80 || got.Unit != "km" {
		t.Errorf("%+v %v", got, err)
	}
	for _, c := range []struct{ country, in, want string }{
		{"GB", "M1 1AE", "M1"}, {"GB", "M11AE", "M1"}, {"GB", "SW1A1AA", "SW1A"}, {"GB", "SW1A", "SW1A"}, {"GB", "B33 8TH", "B33"},
		{"IE", "D02 X285", "D02"}, {"IE", "D02X285", "D02"}, {"CA", "K1A 0B1", "K1A"}, {"NL", "1012 AB", "1012"},
		{"US", "94102-1234", "94102"}, {"US", "94102", "94102"}, {"DE", "10115", "10115"},
	} {
		got, err := Clean(Location{PostalCode: c.in, CountryCode: c.country})
		if err != nil || got.PostalCode != c.want {
			t.Errorf("%s %q: %q %v", c.country, c.in, got.PostalCode, err)
		}
		// Cleaning it again changes nothing, as a saved one comes back through the form.
		if again, _ := Clean(got); again.PostalCode != c.want {
			t.Errorf("%s %q twice: %q", c.country, c.in, again.PostalCode)
		}
	}
	for _, bad := range []Location{{PostalCode: "SW1A", City: "London", CountryCode: "GB"}, {CountryCode: "GB"}, {City: "X", CountryCode: "Britain"}, {PostalCode: "<script>", CountryCode: "GB"}, {City: "X", CountryCode: "GB", Distance: 9000}, {City: "X", CountryCode: "GB", Unit: "leagues"}, {City: "X", CountryCode: "GB", Distance: -1}} {
		if _, err := Clean(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestGeoHash(t *testing.T) {
	// A worked example from the format's definition.
	if got := geoHash(57.64911, 10.40744, 11); got != "u4pruydqqvj" {
		t.Errorf("got %s", got)
	}
}

// ---------- review fixes ----------

var sfLoc = Location{PostalCode: "94102", CountryCode: "US", Distance: 50, Unit: "miles"}
var ldnLoc = Location{City: "London", CountryCode: "GB", Distance: 50, Unit: "miles"}

func sfEvent(id string, acts ...map[string]any) map[string]any {
	return tmEventJSON(id, "2026-12-01", "America/Los_Angeles", "Hall", "San Francisco", "37.78", "-122.40", acts...)
}

func openFor(t *testing.T, w *world) *cache {
	t.Helper()
	c, err := w.svc.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A hung source mustn't hold up what's cached, or stack up the people waiting for it.
func TestNearSlowSearchHoldsNobodyElse(t *testing.T) {
	w := newWorld(t)
	w.tm.near = []map[string]any{tmEventJSON("L", "2026-12-01", "Europe/London", "O2", "London", "51.5", "0", tmAttr("A", "Act", 1, ""))}
	c := openFor(t, w)
	if got := w.svc.Near(context.Background(), c, ldnLoc); len(got.Shows) != 1 {
		t.Fatalf("seed: %+v", got)
	}
	calls := w.tm.callCount()
	release := make(chan struct{})
	w.tm.mu.Lock()
	w.tm.hold = release
	w.tm.mu.Unlock()
	defer func() {
		w.tm.mu.Lock()
		w.tm.hold = nil
		w.tm.mu.Unlock()
		close(release)
	}()
	// Several people at the location that's hung, each waiting only as long as they can.
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			if res := w.svc.Near(ctx, c, sfLoc); res.Error == "" {
				t.Error("no message while the source hangs")
			}
		}()
	}
	wg.Wait()
	w.tm.mu.Lock()
	sent := len(w.tm.calls) - calls
	w.tm.mu.Unlock()
	if sent != 1 {
		t.Errorf("%d calls for one place", sent)
	}
	// Someone with a cached result doesn't wait at all.
	start := time.Now()
	if got := w.svc.Near(context.Background(), c, ldnLoc); len(got.Shows) != 1 || got.Error != "" {
		t.Errorf("cached: %+v", got)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("a cached read took %v", d)
	}
}

func TestNearFailureCoolsDown(t *testing.T) {
	w := newWorld(t)
	c := openFor(t, w)
	w.tm.status = 500
	if res := w.svc.Near(context.Background(), c, sfLoc); res.Error == "" {
		t.Fatal("no message")
	}
	calls := w.tm.callCount()
	if res := w.svc.Near(context.Background(), c, sfLoc); res.Error == "" {
		t.Error("the message went during the cooldown")
	}
	if n := w.tm.callCount(); n != calls {
		t.Errorf("%d calls during the cooldown", n-calls)
	}
	w.now = w.now.Add(failCooldown + time.Second)
	w.svc.Near(context.Background(), c, sfLoc)
	if n := w.tm.callCount(); n == calls {
		t.Error("not tried again after the cooldown")
	}
}

func TestAKnownPointSurvivesAFailedOrEmptyRefresh(t *testing.T) {
	w := newWorld(t)
	c := openFor(t, w)
	w.tm.near = []map[string]any{sfEvent("E", tmAttr("A", "Act", 1, ""))}
	first, err := w.svc.Locate(context.Background(), c, sfLoc)
	if err != nil || !first.Found {
		t.Fatalf("first: %+v %v", first, err)
	}
	// Due again, and the source is failing: the point stands, and so does the search's key.
	w.now = w.now.Add(placeTTL + time.Hour)
	w.tm.status = 500
	p, err := w.svc.Locate(context.Background(), c, sfLoc)
	if err != nil || !p.Found || p.Lat != first.Lat {
		t.Errorf("on a failure: %+v %v", p, err)
	}
	// Due again, and nothing's on: still the point, and not asked about for another month.
	w.tm.status, w.tm.near = 0, nil
	w.now = w.now.Add(failCooldown + time.Second)
	if p, err = w.svc.Locate(context.Background(), c, sfLoc); err != nil || !p.Found || p.Lat != first.Lat {
		t.Errorf("on a miss: %+v %v", p, err)
	}
	calls := w.tm.callCount()
	if p, _ = w.svc.Locate(context.Background(), c, sfLoc); !p.Found || w.tm.callCount() != calls {
		t.Errorf("after a miss: %+v, %d more calls", p, w.tm.callCount()-calls)
	}
}

func TestPrunedPlaces(t *testing.T) {
	w := newWorld(t)
	c := openFor(t, w)
	ctx := context.Background()
	c.savePlace(ctx, "recent", place{Found: true, CheckedAt: w.now.Add(-placeTTL - time.Hour)})
	c.savePlace(ctx, "unused", place{Found: true, CheckedAt: w.now.Add(-placeKeep - time.Hour)})
	c.savePlace(ctx, "miss", place{CheckedAt: w.now.Add(-placeMissTTL - time.Hour)})
	if _, err := c.prune(ctx, w.now); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.place(ctx, "recent"); !ok {
		t.Error("a point due a refresh went: it's what a failed refresh falls back on")
	}
	for _, key := range []string{"unused", "miss"} {
		if _, ok := c.place(ctx, key); ok {
			t.Errorf("%s stayed", key)
		}
	}
}

// Artists that always fail mustn't stop the rest, or be asked about on every run.
func TestFailingArtistsDontStopTheJob(t *testing.T) {
	w := newWorld(t)
	for i := range 5 {
		name := fmt.Sprint("Bad ", i)
		w.mark(w.artist(w.lib, name, ""), "rating", 8.0) // first in the queue
		if w.tm.failFor == nil {
			w.tm.failFor = map[string]bool{}
		}
		w.tm.failFor[name] = true
	}
	w.artist(w.lib, "Good", "")
	w.tm.attractions["Good"] = []map[string]any{tmAttr("G", "Good", 0, "")}
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(w.tm.calls, "/attractions.json Good") {
		t.Errorf("never reached the artist behind them: %v", w.tm.calls)
	}
	if st := w.svc.Status(context.Background()); st.LastError == "" {
		t.Error("the failures should show in the status")
	}
	// They wait their turn, on the job and when their page is opened.
	calls := w.tm.callCount()
	w.now = w.now.Add(time.Hour)
	w.svc.Sync(context.Background())
	w.svc.RefreshArtist(context.Background(), "Bad 0", "")
	if n := w.tm.callCount(); n != calls {
		t.Errorf("%d calls before the retry", n-calls)
	}
	w.now = w.now.Add(artistRetry)
	w.svc.Sync(context.Background())
	if n := w.tm.callCount(); n != calls+5 {
		t.Errorf("%d calls at the retry", n-calls)
	}
	// A success ends it.
	delete(w.tm.failFor, "Bad 0")
	w.now = w.now.Add(artistRetry + time.Hour)
	w.svc.Sync(context.Background())
	if m, _ := w.svc.cur.Load().match(context.Background(), artistKey("Bad 0", "")); !m.Retry.IsZero() {
		t.Errorf("still waiting: %+v", m)
	}
}

// The network going away is the source's problem, not each artist's: stop, and hold nobody back.
func TestUnreachableStillStopsARun(t *testing.T) {
	w := newWorld(t)
	for _, n := range []string{"A", "B", "C", "D", "E"} {
		w.artist(w.lib, n, "")
	}
	openFor(t, w)
	w.tm.Close()
	msg, _ := w.svc.Sync(context.Background())
	if !strings.Contains(msg, "then stopped") {
		t.Errorf("message: %q", msg)
	}
	if b := w.svc.budget.snapshot(); b.Background != syncMaxFailures {
		t.Errorf("%d calls", b.Background)
	}
	m, _ := w.svc.cur.Load().matches(context.Background())
	for k, r := range m {
		if !r.Retry.IsZero() {
			t.Errorf("%s was held back: %+v", k, r)
		}
	}
}

func TestBadKeyIsLatchedUntilItChanges(t *testing.T) {
	w := newWorld(t)
	w.artist(w.lib, "A", "")
	c := openFor(t, w)
	w.cfg.TicketmasterKey = "revoked"
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	used := w.svc.budget.snapshot().Used
	if used != 1 {
		t.Fatalf("%d calls with a bad key", used)
	}
	// Nothing else is sent or counted: not the job, not pages, not a search.
	w.svc.Sync(context.Background())
	w.svc.RefreshArtist(context.Background(), "A", "")
	if res := w.svc.Near(context.Background(), c, sfLoc); !strings.Contains(res.Error, "key isn't working") {
		t.Errorf("message: %q", res.Error)
	}
	if b := w.svc.budget.snapshot(); b.Used != used {
		t.Errorf("counted %d refused calls", b.Used-used)
	}
	// A new key starts afresh, with the day's count behind it.
	w.svc.budget.state.Used = hardDailyCap
	w.cfg.TicketmasterKey = testKey
	w.svc.Changed(w.cfg, true)
	if b := w.svc.budget.snapshot(); b.BadKey || b.Used != 0 {
		t.Errorf("after a new key: %+v", b)
	}
	if res := w.svc.Near(context.Background(), w.svc.cur.Load(), sfLoc); res.Error != "" {
		t.Errorf("with the new key: %q", res.Error)
	}
	// Turned off and on with the same key: another try, the day's count standing.
	w.svc.budget.Rejected()
	w.cfg.Enabled = false
	w.svc.Changed(w.cfg, false)
	if b := w.svc.budget.snapshot(); b.BadKey {
		t.Error("still refused after turning it off")
	}
}

func TestReportedQuotaWithoutAResetExpires(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	b := newBudget(func() int { return 100 }, func() time.Time { return now })
	b.Sleep = func(context.Context, time.Duration) error { return nil }
	b.After(Quota{Limit: 5000, Available: 0})
	if err := b.Before(context.Background()); !errors.Is(err, ErrOverBudget) {
		t.Errorf("with none left: %v", err)
	}
	now = now.Add(unknownResetWait + time.Second)
	if err := b.Before(context.Background()); err != nil {
		t.Errorf("after the wait: %v", err)
	}
}

// A favourite playing nearby months away is beyond what the search reaches.
func TestNearListIncludesStoredShowsOfTheirArtists(t *testing.T) {
	w := newWorld(t)
	w.mark(w.artist(w.lib, "Fav", ""), "favorite_at", "2026-01-01T00:00:00Z")
	fav := tmAttr("F", "Fav", 2, "")
	w.tm.attractions["Fav"] = []map[string]any{fav}
	soon, later := sfEvent("soon", fav), tmEventJSON("later", "2027-03-01", "America/Los_Angeles", "Hall", "San Francisco", "37.78", "-122.40", fav)
	far := tmEventJSON("far", "2027-03-02", "America/New_York", "Hall", "New York", "40.72", "-73.99", fav)
	w.tm.shows["F"] = []map[string]any{soon, later, far}
	w.tm.near = []map[string]any{soon} // the search stops short of the later one
	if _, err := w.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	loc := sfLoc
	page, err := w.svc.BuildPage(context.Background(), w.input(items.Access{}, &loc))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, v := range page.Near {
		ids = append(ids, v.ID)
	}
	if !slices.Equal(ids, []string{"soon", "later"}) {
		t.Errorf("near %v", ids)
	}
	if page.Near[1].ReasonKind != ReasonFavourite {
		t.Errorf("reason: %+v", page.Near[1])
	}
}

func TestABillAcrossChunksComesBackOnce(t *testing.T) {
	w := newWorld(t)
	c := openFor(t, w)
	ctx := context.Background()
	if err := c.saveNear(ctx, "k", []Show{{ID: "fest", Source: "s", Title: "Festival", URL: "https://x/y", LocalDate: "2026-12-01", Status: "scheduled",
		Acts: []Attraction{{ID: "first", Name: "First"}, {ID: "last", Name: "Last"}}}}, w.now); err != nil {
		t.Fatal(err)
	}
	ids := []string{"first"}
	for i := range sqlChunk {
		ids = append(ids, fmt.Sprint("x", i))
	}
	ids = append(ids, "last")
	got, err := c.showsOfAttractions(ctx, ids)
	if err != nil || len(got) != 1 {
		t.Errorf("%d shows, %v", len(got), err)
	}
}

func TestOnlyABadFileIsRebuilt(t *testing.T) {
	for msg, want := range map[string]bool{
		"database is locked (5)": false, "disk I/O error": false, "database or disk is full": false, "unable to open database file": false,
		"file is not a database (26)": true, "database disk image is malformed": true,
	} {
		if got := shouldRebuild(errors.New(msg)); got != want {
			t.Errorf("%q: %v", msg, got)
		}
	}
	if !shouldRebuild(fmt.Errorf("%w: schema version 9", errRebuild)) {
		t.Error("another version")
	}
}

func TestLocationChangesAreLimited(t *testing.T) {
	w := newWorld(t)
	for i := range maxLocationChanges {
		if !w.svc.AllowLocationChange(1) {
			t.Fatalf("change %d refused", i)
		}
	}
	if w.svc.AllowLocationChange(1) {
		t.Error("over the limit")
	}
	if !w.svc.AllowLocationChange(2) {
		t.Error("someone else was held back")
	}
	w.now = w.now.Add(24 * time.Hour)
	if !w.svc.AllowLocationChange(1) {
		t.Error("the next day")
	}
}
