package intown

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"marquee/internal/items"
)

// ShowView is a show as someone sees it: about one act, with how far it is from them.
type ShowView struct {
	Show
	// Act is who this entry is about; Support says they aren't the headliner. ItemID is their
	// artist page, when they're in a library the person can see.
	Act     string
	Support bool
	ItemID  int64
	// Distance is in Unit, when known. Near means within their distance.
	Distance    float64
	HasDistance bool
	Unit        string
	Near        bool
	// Reason says why it's suggested, on the near list.
	Reason, ReasonKind string
}

// ArtistView is a library artist with upcoming shows.
type ArtistView struct {
	ItemID               int64
	ShowCount, NearCount int
	Shows                []ShowView
	Reason, ReasonKind   string
}

// PageInput is what the page needs from the person's account, all of it limited to what they
// may see except Artists, which is used only to tell whose shows are whose.
type PageInput struct {
	Artists []items.MusicArtist
	Visible map[int64]bool
	Liking  map[int64]*items.ArtistLiking
	// Location is nil when they haven't set one.
	Location *Location
	// Similar is what listeners consider alike, from an artist to others (0–1).
	Similar func(artist int64) map[int64]float64
	// SoundAlike is Soundprint's closest artists to some liked ones, to the liked one.
	SoundAlike func(liked []int64) map[int64]int64
}

// Page is the In Town page.
type Page struct {
	Artists       []ArtistView
	Near          []ShowView
	LocationFound bool
	NearError     string
}

// visibleSet works out one artist id per artist the person can see (their Rep, when an artist
// is in two libraries) and their combined liking.
type visibleSet struct {
	groups map[string]*artistGroup
	rep    map[string]int64
	// repOf maps any visible item to its Rep.
	repOf  map[int64]int64
	liking map[int64]items.ArtistLiking
	names  map[int64]string
}

func (in PageInput) visibleSet() visibleSet {
	v := visibleSet{groups: groupArtists(in.Artists), rep: map[string]int64{}, repOf: map[int64]int64{},
		liking: map[int64]items.ArtistLiking{}, names: map[int64]string{}}
	for _, a := range in.Artists {
		if !in.Visible[a.ID] {
			continue
		}
		key := artistKey(a.Name, a.MBID)
		rep, ok := v.rep[key]
		if !ok {
			rep = a.ID
			v.rep[key] = rep
			v.names[rep] = a.Name
		}
		v.repOf[a.ID] = rep
		if l := in.Liking[a.ID]; l != nil {
			v.liking[rep] = mergeLiking(v.liking[rep], *l)
		} else if _, ok := v.liking[rep]; !ok {
			v.liking[rep] = items.ArtistLiking{}
		}
	}
	return v
}

// resolver says which library artist an act on a bill is, by what's known of both: a stored
// match; a MusicBrainz id; or an exact name that only one library artist and one attraction
// have, and that a lookup hasn't already decided otherwise.
type resolver struct {
	attractionKey map[string]string
	matches       map[string]matchRow
	byMBID        map[string]string
	keysOfName    map[string][]string
	attractionsOf map[string]int
}

func newResolver(groups map[string]*artistGroup, matches map[string]matchRow, attractionCounts map[string]int) resolver {
	r := resolver{attractionKey: map[string]string{}, matches: matches, byMBID: map[string]string{},
		keysOfName: map[string][]string{}, attractionsOf: attractionCounts}
	for key, m := range matches {
		if m.AttractionID != "" {
			r.attractionKey[m.AttractionID] = key
		}
	}
	for key, g := range groups {
		if g.MBID != "" {
			r.byMBID[g.MBID] = key
		}
		n := normalizeName(g.Name)
		r.keysOfName[n] = append(r.keysOfName[n], key)
	}
	return r
}

func (r resolver) key(a Attraction) (string, bool) {
	if key, ok := r.attractionKey[a.ID]; ok {
		return key, true
	}
	for _, id := range a.MBIDs {
		if key, ok := r.byMBID[id]; ok {
			return key, true
		}
	}
	n := normalizeName(a.Name)
	keys := r.keysOfName[n]
	if len(keys) != 1 || r.attractionsOf[n] > 1 {
		return "", false
	}
	key := keys[0]
	// A lookup already found them to be another attraction, or refused to choose.
	if m, ok := r.matches[key]; ok && !m.CheckedAt.IsZero() {
		return "", false
	}
	// The act's id and the artist's disagree.
	if strings.HasPrefix(key, "mbid:") && len(a.MBIDs) > 0 {
		return "", false
	}
	return key, true
}

// decorate fills in distance from the person's point and whether a show is near them. Without a
// point, a show in their own city counts as near.
func decorate(v *ShowView, loc *Location, p place) {
	if loc == nil {
		return
	}
	v.Unit = loc.Unit
	if p.Found && v.Venue.HasPoint {
		miles := distanceMiles(p.Lat, p.Lon, v.Venue.Lat, v.Venue.Lon)
		v.Distance = math.Round(milesTo(miles, loc.Unit)*10) / 10
		v.HasDistance = true
		v.Near = v.Distance <= float64(loc.Distance)
		return
	}
	if loc.City != "" && strings.EqualFold(loc.CountryCode, v.Venue.Country) && normalizeName(loc.City) == normalizeName(v.Venue.City) {
		v.Near = true
	}
}

// forAct makes a show about one act on its bill.
func forAct(s Show, attractionID string) ShowView {
	v := ShowView{Show: s}
	for i, a := range s.Acts {
		if a.ID == attractionID {
			v.Act, v.Support = a.Name, i > 0
			break
		}
	}
	if v.Act == "" && len(s.Acts) > 0 {
		v.Act = s.Acts[0].Name
	}
	if v.Act == "" {
		v.Act = s.Title
	}
	return v
}

// cachedPlace is what's stored of a location's point, with no call made.
func (s *Service) cachedPlace(ctx context.Context, c *cache, loc *Location) place {
	if loc == nil {
		return place{}
	}
	p, _ := c.place(ctx, placeKey(*loc))
	return p
}

// BuildPage makes the page: artists with upcoming shows, then shows near the person.
func (s *Service) BuildPage(ctx context.Context, in PageInput) (Page, error) {
	var page Page
	c, err := s.open(ctx)
	if err != nil {
		return page, err
	}
	v := in.visibleSet()
	matches, err := c.matches(ctx)
	if err != nil {
		s.check(err)
		return page, err
	}
	now := s.now()
	var near nearResult
	if in.Location != nil {
		near = s.Near(ctx, c, *in.Location)
		page.LocationFound = near.Place.Found
		page.NearError = near.Error
	}
	// The upper area: visible library artists with upcoming shows.
	attractions := make([]string, 0, len(v.rep))
	keyOfAttraction := map[string]string{}
	for key := range v.rep {
		if m, ok := matches[key]; ok && m.AttractionID != "" {
			attractions = append(attractions, m.AttractionID)
			keyOfAttraction[m.AttractionID] = key
		}
	}
	slices.Sort(attractions)
	shows, err := c.showsOfAttractions(ctx, attractions)
	if err != nil {
		s.check(err)
		return page, err
	}
	byArtist := map[string][]ShowView{}
	for _, sh := range shows {
		if isPast(sh.LocalDate, sh.TimeZone, now, 0) {
			continue
		}
		for _, a := range sh.Acts {
			key, ok := keyOfAttraction[a.ID]
			if !ok {
				continue
			}
			sv := forAct(sh, a.ID)
			sv.ItemID = v.rep[key]
			decorate(&sv, in.Location, near.Place)
			byArtist[key] = append(byArtist[key], sv)
		}
	}
	page.Artists = artistViews(byArtist, v)

	// The lower area.
	if in.Location != nil {
		attractionCounts, err := c.attractionNames(ctx)
		if err != nil {
			s.check(err)
			return page, err
		}
		near.Shows = withStoredNear(near.Shows, shows, in.Location, near.Place)
		page.Near = s.nearViews(near, in, v, newResolver(v.groups, matches, attractionCounts), now)
	}
	return page, nil
}

// withStoredNear adds the shows of the person's own artists that are near them. The search only
// reaches the soonest few hundred shows, and an artist they love could be months away.
func withStoredNear(found, stored []Show, loc *Location, p place) []Show {
	seen := make(map[string]bool, len(found))
	for _, s := range found {
		seen[s.ID] = true
	}
	for _, s := range stored {
		if seen[s.ID] {
			continue
		}
		v := ShowView{Show: s}
		decorate(&v, loc, p)
		if v.Near {
			seen[s.ID] = true
			found = append(found, s)
		}
	}
	return found
}

// artistViews orders the artists: those liked most first, then the next date. A clear dislike
// isn't offered.
func artistViews(byArtist map[string][]ShowView, v visibleSet) []ArtistView {
	type row struct {
		ArtistView
		score float64
		next  string
		name  string
	}
	rows := make([]row, 0, len(byArtist))
	for key, list := range byArtist {
		item := v.rep[key]
		l := v.liking[item]
		if dislikes(l) {
			continue
		}
		slices.SortFunc(list, func(a, b ShowView) int {
			return cmp.Or(cmp.Compare(a.LocalDate, b.LocalDate), cmp.Compare(a.LocalTime, b.LocalTime), cmp.Compare(a.ID, b.ID))
		})
		r := row{ArtistView: ArtistView{ItemID: item, ShowCount: len(list)}, score: likingScore(l), next: list[0].LocalDate, name: strings.ToLower(v.names[item])}
		for _, s := range list {
			if s.Near {
				r.NearCount++
			}
		}
		r.Shows = list[:min(nextShows, len(list))]
		r.ReasonKind, r.Reason = likedReason(l)
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b row) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.next, b.next), cmp.Compare(a.name, b.name))
	})
	rows = rows[:min(maxPageArtists, len(rows))]
	out := make([]ArtistView, len(rows))
	for i, r := range rows {
		out[i] = r.ArtistView
	}
	return out
}

func (s *Service) nearViews(near nearResult, in PageInput, v visibleSet, r resolver, now time.Time) []ShowView {
	taste := Taste{Liking: v.liking, Names: v.names, Genres: topGenres(v.liking, 5)}
	if in.Similar != nil {
		taste.Similar = func(artist int64) map[int64]float64 {
			out := map[int64]float64{}
			for id, score := range in.Similar(artist) {
				if rep, ok := v.repOf[id]; ok {
					out[rep] = max(out[rep], score)
				}
			}
			return out
		}
	}
	if in.SoundAlike != nil {
		liked := make([]int64, 0, len(v.liking))
		for id, l := range v.liking {
			if likingScore(l) > 0 && !dislikes(l) {
				liked = append(liked, id)
			}
		}
		slices.SortFunc(liked, func(a, b int64) int {
			return cmp.Or(cmp.Compare(likingScore(v.liking[b]), likingScore(v.liking[a])), cmp.Compare(a, b))
		})
		if len(liked) > 0 {
			taste.SoundsLike = map[int64]int64{}
			for id, to := range in.SoundAlike(liked[:min(soundAlikeSeeds, len(liked))]) {
				rep, ok := v.repOf[id]
				if ok && rep != to {
					taste.SoundsLike[rep] = to
				}
			}
		}
	}
	list := make([]nearCandidate, 0, len(near.Shows))
	for _, sh := range near.Shows {
		if isPast(sh.LocalDate, sh.TimeZone, now, 0) {
			continue
		}
		c := nearCandidate{View: forAct(sh, "")}
		decorate(&c.View, in.Location, near.Place)
		// Beyond their distance: the search was rounded up, and others share it.
		if c.View.HasDistance && !c.View.Near {
			continue
		}
		c.View.Near = true
		for _, a := range sh.Acts {
			ref := actRef{Act: a}
			if key, ok := r.key(a); ok {
				ref.ItemID = v.rep[key]
			}
			c.Acts = append(c.Acts, ref)
		}
		if len(c.Acts) > 0 {
			c.View.Act, c.View.ItemID, c.View.Support = c.Acts[0].Act.Name, c.Acts[0].ItemID, false
		}
		list = append(list, c)
	}
	return rankNear(list, taste)
}

// soundAlikeSeeds is how many of someone's most liked artists Soundprint compares with.
const soundAlikeSeeds = 12

// ArtistShows is an artist's upcoming shows and how many are near the person, from what's stored
// and with no call made, so it never slows their page.
func (s *Service) ArtistShows(ctx context.Context, name, mbid string, loc *Location) ([]ShowView, int, error) {
	c, err := s.open(ctx)
	if err != nil {
		return nil, 0, err
	}
	m, err := c.match(ctx, artistKey(name, mbid))
	if err != nil {
		s.check(err)
		return nil, 0, err
	}
	if m.AttractionID == "" {
		return []ShowView{}, 0, nil
	}
	shows, err := c.showsOfAttractions(ctx, []string{m.AttractionID})
	if err != nil {
		s.check(err)
		return nil, 0, err
	}
	p := s.cachedPlace(ctx, c, loc)
	now := s.now()
	out := make([]ShowView, 0, len(shows))
	near := 0
	for _, sh := range shows {
		if isPast(sh.LocalDate, sh.TimeZone, now, 0) {
			continue
		}
		v := forAct(sh, m.AttractionID)
		decorate(&v, loc, p)
		if v.Near {
			near++
		}
		out = append(out, v)
	}
	return out[:min(maxArtistShows, len(out))], near, nil
}

// IsOff reports In Town being off, as against a failure.
func IsOff(err error) bool { return errors.Is(err, ErrOff) }
