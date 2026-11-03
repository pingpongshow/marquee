package intown

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"marquee/internal/items"
	"marquee/internal/settings"
)

const (
	// matchTTL is how long an artist's match lasts, and noMatchTTL how long no match does:
	// acts rarely change identity, and a band that's newly on tour shows up within weeks.
	matchTTL   = 30 * 24 * time.Hour
	noMatchTTL = 14 * 24 * time.Hour
	// showsTTL is about a day. An artist nobody cares for with no shows can wait longer.
	showsTTL      = 24 * time.Hour
	quietShowsTTL = 72 * time.Hour
	// openedRecently puts artists whose pages were opened first in the queue.
	openedRecently = 30 * 24 * time.Hour
	// nearTTL is how long a search by location lasts.
	nearTTL = 4 * time.Hour
	// nearWait is how long a page waits for the source before showing what it has.
	nearWait = 20 * time.Second
	// Pages of a search by location, kept modest: the soonest few hundred shows.
	nearPageSize = 100
	maxNearPages = 3
	// placeTTL is how long a location's point lasts, and placeMissTTL how long not finding one.
	placeTTL     = 30 * 24 * time.Hour
	placeMissTTL = 7 * 24 * time.Hour
	placeSize    = 50
	// placeKeep is how long a found point stays once it's stopped being used. It's longer than
	// placeTTL, or nothing would be there to fall back on when a refresh fails.
	placeKeep = 2 * placeTTL
	// failCooldown is how long a failed search isn't tried again, so a hung or failing source
	// isn't called, and waited for, on every page load.
	failCooldown = 5 * time.Minute
	// artistRetry is how long an artist whose lookup failed waits for another go.
	artistRetry = 24 * time.Hour
	// maxLocationChanges is how many times a day one person can change their location, each
	// of which spends calls.
	maxLocationChanges = 10
	// syncSlice caps one run of the hourly job, which also spreads the budget across the day.
	syncSlice = 200
	// syncMaxFailures stops a run that keeps failing to reach the source, so a bad day costs few
	// calls. Failures of one artist don't count: they wait their turn (artistRetry).
	syncMaxFailures = 3
	// nextShows is how many shows the page gives each artist.
	nextShows = 3
	// maxPageArtists is how many artists the page lists.
	maxPageArtists = 60
	// maxArtistShows is how many of an artist's shows are returned.
	maxArtistShows = 100
)

// ErrOff means In Town isn't on.
var ErrOff = errors.New("In Town is off")

// Service runs In Town: background lookups, searches by location and the cache file.
type Service struct {
	// Path is the cache file.
	Path string
	// Settings reads the admin's settings.
	Settings func() settings.InTown
	Items    *items.Store
	// Base replaces Ticketmaster's address (MARQUEE_TICKETMASTER_URL), for testing.
	Base string
	// Source replaces Ticketmaster altogether (tests).
	Source Source
	Now    func() time.Time

	once   sync.Once
	budget *budget
	source Source

	mu  sync.Mutex // opening and closing the cache
	cur atomic.Pointer[cache]

	running atomic.Bool

	flightMu sync.Mutex
	flights  map[string]*flight
	failures map[string]failure

	changesMu sync.Mutex
	changes   map[int64]locationChanges

	refreshMu  sync.Mutex
	refreshing map[string]bool
	done, all  atomic.Int64
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) setup() {
	s.once.Do(func() {
		s.budget = newBudget(func() int {
			if b := s.Settings().DailyBudget; b > 0 {
				return b
			}
			return DefaultDailyBudget
		}, s.now)
		s.budget.Saved = func(st budgetState) {
			if c := s.cur.Load(); c != nil {
				c.saveBudget(context.Background(), st)
			}
		}
		s.source = s.Source
		if s.source == nil {
			s.source = &Ticketmaster{Key: func() string { return s.Settings().TicketmasterKey }, Base: s.Base, Limiter: s.budget}
		}
		s.refreshing = map[string]bool{}
		s.flights, s.failures = map[string]*flight{}, map[string]failure{}
		s.changes = map[int64]locationChanges{}
	})
}

// Enabled reports whether In Town is on and has a key.
func (s *Service) Enabled() bool { return s != nil && s.Settings().Active() }

// open returns the cache, opening it when needed.
func (s *Service) open(ctx context.Context) (*cache, error) {
	s.setup()
	if !s.Enabled() {
		return nil, ErrOff
	}
	if c := s.cur.Load(); c != nil {
		return c, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.cur.Load(); c != nil {
		return c, nil
	}
	if !s.Enabled() { // turned off just now: don't bring the file back
		return nil, ErrOff
	}
	c, err := openCache(ctx, s.Path)
	if err != nil {
		return nil, err
	}
	if st, ok := c.loadBudget(ctx); ok {
		s.budget.restore(st)
	}
	s.cur.Store(c)
	return c, nil
}

// check starts the cache afresh when an error shows the file is bad. Everything in it can be
// fetched again.
func (s *Service) check(err error) {
	if !isCorrupt(err) {
		return
	}
	slog.Warn("in town: the cache file is damaged, starting a new one")
	s.closeCache(true)
}

func (s *Service) closeCache(remove bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.cur.Swap(nil); c != nil {
		c.close()
	}
	if remove {
		removeCacheFiles(s.Path)
	}
}

// Start runs at startup: it deletes the cache if In Town has been turned off, and past shows
// if it's on.
func (s *Service) Start(ctx context.Context) {
	s.setup()
	if !s.Enabled() {
		removeCacheFiles(s.Path)
		return
	}
	if msg, err := s.Prune(ctx); err != nil {
		slog.Warn("in town: remove past shows", "err", err)
	} else {
		slog.Debug("in town", "result", msg)
	}
}

// Changed is told when the settings change: turned off, the cache file goes; a new key starts
// with a clean slate.
func (s *Service) Changed(cfg settings.InTown, keyChanged bool) {
	s.setup()
	s.clearFailures()
	if !cfg.Active() {
		s.closeCache(true)
		s.budget.clearBadKey()
		return
	}
	if keyChanged {
		s.budget.newKey()
		if c := s.cur.Load(); c != nil {
			c.setMeta(context.Background(), "last_error", "")
		}
	}
}

// Prune deletes shows whose date has passed; the daily task.
func (s *Service) Prune(ctx context.Context) (string, error) {
	c, err := s.open(ctx)
	if errors.Is(err, ErrOff) {
		return "In Town is off", nil
	}
	if err != nil {
		return "", err
	}
	n, err := c.prune(ctx, s.now())
	if err != nil {
		s.check(err)
		return "", err
	}
	return fmt.Sprintf("Removed %d past shows", n), nil
}

// ---------- errors ----------

var keyInURL = regexp.MustCompile(`(?i)(apikey=)[^&\s"']+`)

// redact takes the key out of anything on its way to a log or an admin's screen. The source's
// errors shouldn't carry it, so this is a second line of defence.
func (s *Service) redact(msg string) string {
	msg = keyInURL.ReplaceAllString(msg, "${1}REDACTED")
	if k := s.Settings().TicketmasterKey; k != "" {
		msg = strings.ReplaceAll(msg, k, "REDACTED")
	}
	return msg
}

// noteError records the last problem for the status line.
func (s *Service) noteError(ctx context.Context, c *cache, err error) {
	msg := ""
	if err != nil {
		msg = s.redact(err.Error())
		slog.Warn("in town", "err", msg)
	}
	c.setMeta(context.WithoutCancel(ctx), "last_error", msg)
}

// ---------- library artists ----------

// artistGroup is one artist across libraries.
type artistGroup struct {
	Key, Name, MBID string
	Cared           bool
	NameShared      bool
	IDs             []int64
}

// groupArtists gathers the library's artists by key, and notes names shared by different keys.
func groupArtists(list []items.MusicArtist) map[string]*artistGroup {
	out := map[string]*artistGroup{}
	keysOfName := map[string]map[string]bool{}
	for _, a := range list {
		key := artistKey(a.Name, a.MBID)
		g := out[key]
		if g == nil {
			g = &artistGroup{Key: key, Name: a.Name, MBID: a.MBID}
			out[key] = g
		}
		g.IDs = append(g.IDs, a.ID)
		g.Cared = g.Cared || a.Cared
		n := normalizeName(a.Name)
		if keysOfName[n] == nil {
			keysOfName[n] = map[string]bool{}
		}
		keysOfName[n][key] = true
	}
	for _, g := range out {
		g.NameShared = len(keysOfName[normalizeName(g.Name)]) > 1
	}
	return out
}

// ---------- the background job ----------

// Progress reports the running job's progress.
func (s *Service) Progress() (int, int) { return int(s.done.Load()), int(s.all.Load()) }

// dueWork lists what the job should look up, most wanted first: artists whose pages people
// opened recently, then those someone favourited, rated or played, then the rest, each
// starting from whoever has waited longest.
func dueWork(groups map[string]*artistGroup, matches map[string]matchRow, upcoming map[string]bool, now time.Time) []*artistGroup {
	type item struct {
		g    *artistGroup
		tier int
		at   time.Time
	}
	var due []item
	for key, g := range groups {
		m := matches[key]
		if now.Before(m.Retry) {
			continue
		}
		needMatch, needShows := matchDue(m, now), showsDue(m, g.Cared, upcoming[m.AttractionID], now)
		if !needMatch && !needShows {
			continue
		}
		it := item{g: g, tier: 2, at: m.CheckedAt}
		if needShows && !needMatch {
			it.at = m.ShowsAt
		}
		if g.Cared {
			it.tier = 1
		}
		if !m.Opened.IsZero() && now.Sub(m.Opened) < openedRecently {
			it.tier = 0
		}
		due = append(due, it)
	}
	slices.SortFunc(due, func(a, b item) int {
		return cmp.Or(cmp.Compare(a.tier, b.tier), a.at.Compare(b.at), cmp.Compare(a.g.Key, b.g.Key))
	})
	out := make([]*artistGroup, len(due))
	for i, it := range due {
		out[i] = it.g
	}
	return out
}

func matchDue(m matchRow, now time.Time) bool {
	if m.CheckedAt.IsZero() {
		return true
	}
	ttl := matchTTL
	if m.AttractionID == "" {
		ttl = noMatchTTL
	}
	return now.Sub(m.CheckedAt) >= ttl
}

func showsDue(m matchRow, cared, upcoming bool, now time.Time) bool {
	if m.AttractionID == "" {
		return false
	}
	ttl := showsTTL
	if !cared && !upcoming {
		ttl = quietShowsTTL
	}
	return now.Sub(m.ShowsAt) >= ttl
}

// Sync is the hourly job: it goes through the library's artists a little at a time, inside the
// day's budget, leaving room for what people do.
func (s *Service) Sync(ctx context.Context) (string, error) {
	c, err := s.open(ctx)
	if errors.Is(err, ErrOff) {
		return "In Town is off", nil
	}
	if err != nil {
		return "", err
	}
	s.running.Store(true)
	defer s.running.Store(false)
	list, err := s.Items.MusicArtists(ctx)
	if err != nil {
		return "", err
	}
	groups := groupArtists(list)
	keep := make(map[string]bool, len(groups))
	for k := range groups {
		keep[k] = true
	}
	if err := c.dropMatches(ctx, keep); err != nil {
		s.check(err)
		return "", err
	}
	matches, err := c.matches(ctx)
	if err != nil {
		s.check(err)
		return "", err
	}
	upcoming, err := s.attractionsWithShows(ctx, c)
	if err != nil {
		s.check(err)
		return "", err
	}
	now := s.now()
	due := dueWork(groups, matches, upcoming, now)
	bg := background(ctx)
	checked, failures, failedArtists := 0, 0, 0
	s.done.Store(0)
	s.all.Store(int64(len(due)))
	defer func() { s.done.Store(0); s.all.Store(0) }()
	used := 0
	var problem error
	for _, g := range due {
		if ctx.Err() != nil || !s.Enabled() || used >= syncSlice || s.budget.remaining() == 0 {
			break
		}
		before := s.budget.snapshot().Background
		err := s.refresh(bg, c, g, matches[g.Key])
		used += s.budget.snapshot().Background - before
		s.done.Add(1)
		if err == nil {
			checked++
			failures = 0
			continue
		}
		// Held back by the budget, or stopped: nothing to report, and nothing to do until later.
		if errors.Is(err, ErrOverBudget) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			break
		}
		s.check(err)
		if s.cur.Load() != c { // the file was bad, or In Town was turned off
			break
		}
		s.noteError(ctx, c, err)
		if artistOnly(err) {
			failedArtists++
			if err := c.setRetry(context.WithoutCancel(ctx), g.Key, s.now().Add(artistRetry)); err != nil {
				s.check(err)
			}
			continue
		}
		var limited *RateLimitError
		if failures++; errors.Is(err, ErrBadKey) || errors.As(err, &limited) || failures >= syncMaxFailures {
			problem = err
			break
		}
	}
	if s.cur.Load() == nil {
		return "In Town was turned off", nil
	}
	if problem == nil && checked > 0 && failures == 0 && failedArtists == 0 {
		s.noteError(ctx, c, nil)
	}
	c.setMeta(context.WithoutCancel(ctx), "last_sync", stamp(s.now()))
	switch {
	case problem != nil:
		return fmt.Sprintf("Looked up %d artists, then stopped: %s", checked, s.redact(problem.Error())), nil
	case len(due) > checked:
		return fmt.Sprintf("Looked up %d artists; %d left for later", checked, len(due)-checked), nil
	}
	return fmt.Sprintf("Looked up %d artists", checked), nil
}

// artistOnly reports a failure that says nothing about the source or the key, so it may be down
// to this artist: the others can still go ahead.
func artistOnly(err error) bool {
	var limited *RateLimitError
	return !errors.Is(err, ErrBadKey) && !errors.Is(err, ErrUnreachable) && !errors.As(err, &limited)
}

func (s *Service) attractionsWithShows(ctx context.Context, c *cache) (map[string]bool, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT attraction_id FROM show_acts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// refresh brings one artist up to date: their match if it's due, then their shows if they are.
// The attraction's own count of upcoming shows can save asking for them.
func (s *Service) refresh(ctx context.Context, c *cache, g *artistGroup, m matchRow) error {
	now := s.now()
	upcomingKnown := -1
	if matchDue(m, now) {
		found, err := s.source.Attractions(ctx, g.Name)
		if err != nil {
			return err
		}
		a, ok := matchAttraction(Artist{Name: g.Name, MBID: g.MBID, NameShared: g.NameShared}, found)
		var seen []Attraction
		want := normalizeName(g.Name)
		for _, f := range found {
			if f.Music && normalizeName(f.Name) == want {
				seen = append(seen, f)
			}
		}
		if ok {
			if !slices.ContainsFunc(seen, func(f Attraction) bool { return f.ID == a.ID }) {
				seen = append(seen, a)
			}
			upcomingKnown = a.Upcoming
		}
		id := ""
		if ok {
			id = a.ID
		}
		if m.AttractionID != "" && m.AttractionID != id {
			// No longer that attraction: its shows aren't this artist's.
			if _, err := c.db.ExecContext(ctx, `DELETE FROM show_acts WHERE attraction_id = ?`, m.AttractionID); err != nil {
				return err
			}
		}
		if err := c.saveMatch(ctx, g.Key, id, seen, now); err != nil {
			return err
		}
		m.AttractionID, m.CheckedAt = id, now
		if id == "" {
			return nil
		}
		m.ShowsAt = time.Time{}
	}
	if !showsDue(m, g.Cared, true, now) {
		return nil
	}
	var shows []Show
	if upcomingKnown != 0 {
		var err error
		if shows, err = s.source.ArtistShows(ctx, m.AttractionID); err != nil {
			return err
		}
	}
	return c.replaceArtistShows(ctx, g.Key, m.AttractionID, shows, now)
}

// RefreshArtist looks an artist up in the background when someone opens their page and what's
// stored is stale. It never holds the page up: the caller doesn't wait.
func (s *Service) RefreshArtist(ctx context.Context, name, mbid string) {
	s.setup()
	key := artistKey(name, mbid)
	s.refreshMu.Lock()
	if s.refreshing[key] {
		s.refreshMu.Unlock()
		return
	}
	s.refreshing[key] = true
	s.refreshMu.Unlock()
	defer func() {
		s.refreshMu.Lock()
		delete(s.refreshing, key)
		s.refreshMu.Unlock()
	}()
	if s.budget.badKey() {
		return
	}
	c, err := s.open(ctx)
	if err != nil {
		return
	}
	if err := c.touchOpened(ctx, key, s.now()); err != nil {
		s.check(err)
		return
	}
	m, err := c.match(ctx, key)
	if err != nil {
		s.check(err)
		return
	}
	if s.now().Before(m.Retry) || (!matchDue(m, s.now()) && !showsDue(m, true, true, s.now())) {
		return
	}
	list, err := s.Items.MusicArtists(ctx)
	if err != nil {
		return
	}
	g := groupArtists(list)[key]
	if g == nil {
		return
	}
	if err := s.refresh(ctx, c, g, m); err != nil && !errors.Is(err, ErrOverBudget) && ctx.Err() == nil {
		s.check(err)
		s.noteError(ctx, c, err)
		if artistOnly(err) {
			c.setRetry(context.WithoutCancel(ctx), key, s.now().Add(artistRetry))
		}
	}
}

// ---------- locations ----------

// locationChanges counts someone's changes on one day.
type locationChanges struct {
	day   string
	count int
}

// AllowLocationChange counts a change of someone's location and says whether it's allowed. Each
// new location spends calls, and the quota is shared, so one person can't use it all up.
func (s *Service) AllowLocationChange(userID int64) bool {
	s.setup()
	s.changesMu.Lock()
	defer s.changesMu.Unlock()
	day := s.now().UTC().Format(time.DateOnly)
	n := s.changes[userID]
	if n.day != day {
		n = locationChanges{day: day}
	}
	if n.count >= maxLocationChanges {
		return false
	}
	n.count++
	s.changes[userID] = n
	return true
}

// flight is a call being made, which others can wait for.
type flight struct {
	done chan struct{}
	err  error
}

// failure is a call that failed, remembered for a few minutes.
type failure struct {
	at  time.Time
	err error
}

// callOnce makes a call for key, or waits for the one already being made, for as long as ctx lets
// it. The call runs apart from whoever asked, since others may be waiting on it, and is bound
// by nearWait. A failure is remembered (see cooling).
func (s *Service) callOnce(ctx context.Context, c *cache, key string, call func(ctx context.Context) error) error {
	s.flightMu.Lock()
	f := s.flights[key]
	if f == nil {
		f = &flight{done: make(chan struct{})}
		s.flights[key] = f
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), nearWait)
		go func() {
			defer cancel()
			f.err = call(run)
			s.flightMu.Lock()
			delete(s.flights, key)
			if f.err != nil {
				s.failures[key] = failure{at: s.now(), err: f.err}
			} else {
				delete(s.failures, key)
			}
			s.flightMu.Unlock()
			if f.err != nil && !errors.Is(f.err, ErrOverBudget) {
				s.noteError(run, c, f.err)
			}
			close(f.done)
		}()
	}
	s.flightMu.Unlock()
	select {
	case <-f.done:
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// cooling is the error from a recent failure of this call, if it shouldn't be tried again yet.
func (s *Service) cooling(key string) error {
	s.flightMu.Lock()
	defer s.flightMu.Unlock()
	if f, ok := s.failures[key]; ok && s.now().Sub(f.at) < failCooldown {
		return f.err
	}
	return nil
}

func (s *Service) clearFailures() {
	s.flightMu.Lock()
	defer s.flightMu.Unlock()
	s.failures = map[string]failure{}
}

// Locate finds a location's point from the venues there, since the source has no way to ask.
// Remembered for a month, or a week when nothing is there. A point already known is kept when a
// refresh fails or finds nothing, as it's still right and everything near them depends on it.
func (s *Service) Locate(ctx context.Context, c *cache, l Location) (place, error) {
	key := placeKey(l)
	read := context.WithoutCancel(ctx)
	prev, have := c.place(read, key)
	if have {
		ttl := placeTTL
		if !prev.Found {
			ttl = placeMissTTL
		}
		if s.now().Sub(prev.CheckedAt) < ttl {
			return prev, nil
		}
	}
	known := have && prev.Found
	flightKey := "place:" + key
	err := s.cooling(flightKey)
	if err == nil {
		err = s.callOnce(ctx, c, flightKey, func(run context.Context) error {
			p, err := s.lookUpPlace(run, l)
			if err != nil {
				return err
			}
			if !p.Found && known {
				p.Found, p.Lat, p.Lon = true, prev.Lat, prev.Lon
			}
			return c.savePlace(context.WithoutCancel(run), key, p)
		})
	}
	if err != nil {
		if known {
			return prev, nil
		}
		return place{CheckedAt: s.now()}, err
	}
	p, _ := c.place(read, key)
	return p, nil
}

// lookUpPlace asks the source about a place: close by, then wider for a postcode.
func (s *Service) lookUpPlace(ctx context.Context, l Location) (place, error) {
	var queries []NearQuery
	base := NearQuery{CountryCode: l.CountryCode, Unit: "miles", Size: placeSize, Radius: 10}
	if l.PostalCode != "" {
		q := base
		q.PostalCode = l.PostalCode
		wider := q
		wider.Radius = 50
		queries = append(queries, q, wider)
	} else {
		q := base
		q.City = l.City
		queries = append(queries, q)
	}
	p := place{CheckedAt: s.now()}
	for _, q := range queries {
		shows, _, err := s.source.Near(ctx, q)
		if err != nil {
			return p, err
		}
		if lat, lon, ok := pointOf(shows, q.City); ok {
			p.Found, p.Lat, p.Lon = true, lat, lon
			break
		}
	}
	return p, nil
}

// LocationSet looks up a new location's point in the background, so its first search is quick.
func (s *Service) LocationSet(ctx context.Context, l Location) {
	c, err := s.open(ctx)
	if err != nil {
		return
	}
	s.Locate(ctx, c, l) // a failure is noted, and asked again when the page is opened
}

// near is a search by location: what's there and whether its point is known.
type nearResult struct {
	Shows []Show
	Place place
	Error string
}

// userMessage turns a failed call into words for the page.
func (s *Service) userMessage(err error) string {
	var limited *RateLimitError
	switch {
	case errors.Is(err, ErrBadKey):
		return "Shows near you aren't available right now: the server's Ticketmaster key isn't working."
	case errors.Is(err, ErrOverBudget), errors.As(err, &limited):
		return "Shows near you may be out of date: the server has used its Ticketmaster calls for now."
	}
	return "Shows near you may be out of date: Ticketmaster couldn't be reached."
}

// Near searches by location, cached per rounded location for a few hours. On a failure it gives
// whatever it last found, with a message saying so. What's cached is read straight off, and one
// location's call never holds up another's.
func (s *Service) Near(ctx context.Context, c *cache, l Location) nearResult {
	var res nearResult
	// The wait is only for the source: what's stored is still read after it runs out.
	read := ctx
	wait, cancel := context.WithTimeout(ctx, nearWait)
	defer cancel()
	p, err := s.Locate(wait, c, l)
	if err != nil {
		res.Error = s.userMessage(err)
	}
	res.Place = p
	q := NearQuery{CountryCode: l.CountryCode, Unit: "miles", Size: nearPageSize, Radius: roundRadius(int(toMiles(float64(l.Distance), l.Unit) + 0.5))}
	key := fmt.Sprintf("loc:%s:%d", placeKey(l), q.Radius)
	if p.Found {
		q.GeoHash = geoHash(p.Lat, p.Lon, geoHashPrecision)
		q.CountryCode = ""
		key = fmt.Sprintf("geo:%s:%d", q.GeoHash, q.Radius)
	} else {
		q.PostalCode, q.City = l.PostalCode, l.City
	}
	if s.now().Sub(c.nearFetchedAt(read, key)) >= nearTTL && res.Error == "" {
		flightKey := "near:" + key
		err := s.cooling(flightKey)
		if err == nil {
			err = s.callOnce(wait, c, flightKey, func(run context.Context) error { return s.fetchNear(run, c, key, q) })
		}
		if err != nil {
			res.Error = s.userMessage(err)
		}
	}
	shows, err := c.showsOfSearch(read, key)
	if err != nil {
		s.check(err)
		res.Error = s.userMessage(err)
	}
	res.Shows = shows
	return res
}

// fetchNear gets a search's pages and stores them.
func (s *Service) fetchNear(ctx context.Context, c *cache, key string, q NearQuery) error {
	var found []Show
	for page := 0; page < maxNearPages; page++ {
		q.Page = page
		list, pages, err := s.source.Near(ctx, q)
		if err != nil {
			return err
		}
		found = append(found, list...)
		if page+1 >= pages {
			break
		}
	}
	err := c.saveNear(context.WithoutCancel(ctx), key, found, s.now())
	s.check(err)
	return err
}

// ---------- status ----------

// Status is for the admin's status line.
type Status struct {
	Running                      bool
	LastSync                     time.Time
	ArtistsTotal                 int
	ArtistsChecked, ArtistsMatch int
	ShowsCached                  int
	CallsToday                   int
	QuotaLimit, QuotaRemaining   int
	Backoff                      time.Time
	LastError                    string
}

// Status reads how things stand. Off, there's nothing to read: the file is gone.
func (s *Service) Status(ctx context.Context) Status {
	s.setup()
	st := Status{QuotaLimit: -1, QuotaRemaining: -1, Running: s.running.Load()}
	if list, err := s.Items.MusicArtists(ctx); err == nil {
		st.ArtistsTotal = len(groupArtists(list))
	}
	c, err := s.open(ctx)
	if err != nil {
		return st
	}
	st.LastSync = parseTime(c.meta(ctx, "last_sync"))
	st.LastError = s.redact(c.meta(ctx, "last_error"))
	st.ArtistsChecked, st.ArtistsMatch, st.ShowsCached = c.counts(ctx)
	b := s.budget.snapshot()
	st.CallsToday, st.QuotaLimit, st.QuotaRemaining = b.Used, b.Limit, b.Available
	if b.Backoff.After(s.now()) {
		st.Backoff = b.Backoff
	}
	return st
}
