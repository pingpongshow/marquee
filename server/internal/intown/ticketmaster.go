package intown

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultTicketmasterBase is the Discovery API v2. MARQUEE_TICKETMASTER_URL replaces it, for
// testing against a fake.
const DefaultTicketmasterBase = "https://app.ticketmaster.com/discovery/v2"

const (
	// attractionsPageSize is big enough to find an act behind its tribute bands: searches
	// are by keyword and the exact name isn't reliably first.
	attractionsPageSize = 100
	artistShowsPageSize = 100
	// maxBody bounds a response, since one event can run to tens of kilobytes.
	maxBody = 16 << 20
	// spikeWait is how long to hold off after a per-second limit, which clears quickly.
	spikeWait = time.Minute
)

// Ticketmaster is the Discovery API v2.
type Ticketmaster struct {
	// Key returns the admin's Consumer Key. It only ever goes in the request's URL, and
	// errors never carry that URL.
	Key     func() string
	Base    string
	Limiter Limiter
	HTTP    *http.Client
}

func (t *Ticketmaster) Name() string { return "ticketmaster" }

func (t *Ticketmaster) client() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		// Nothing here should redirect, and following one would take the key along.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// get fetches path into out. Its errors are safe to log and to show an admin: they never
// include the URL, which has the key in it.
func (t *Ticketmaster) get(ctx context.Context, path string, q url.Values, out any) error {
	if t.Limiter != nil {
		if err := t.Limiter.Before(ctx); err != nil {
			return err
		}
	}
	base := t.Base
	if base == "" {
		base = DefaultTicketmasterBase
	}
	q.Set("apikey", t.Key())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return errors.New("couldn't build the request to Ticketmaster")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := t.client().Do(req)
	if err != nil {
		return safeError(ctx, err)
	}
	defer resp.Body.Close()
	quota := parseQuota(resp.Header)
	if t.Limiter != nil {
		t.Limiter.After(quota)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		if t.Limiter != nil {
			t.Limiter.Rejected()
		}
		return ErrBadKey
	case resp.StatusCode == http.StatusTooManyRequests:
		until := limitedUntil(resp, quota)
		if t.Limiter != nil {
			t.Limiter.Limited(until)
		}
		return &RateLimitError{Until: until}
	default:
		return fmt.Errorf("Ticketmaster answered HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return errors.New("Ticketmaster's answer couldn't be read")
	}
	return nil
}

// safeError drops the URL from a failed request's error, since it has the key in it.
func safeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var u *url.Error
	if errors.As(err, &u) {
		err = u.Err
	}
	return fmt.Errorf("%w: %v", ErrUnreachable, err)
}

// parseQuota reads the Rate-Limit headers, which come on every response. The reset is in
// milliseconds since the epoch; the docs also describe it as a UTC date and time.
func parseQuota(h http.Header) Quota {
	q := Quota{Limit: -1, Available: -1}
	if n, err := strconv.Atoi(h.Get("Rate-Limit")); err == nil {
		q.Limit = n
	}
	if n, err := strconv.Atoi(h.Get("Rate-Limit-Available")); err == nil {
		q.Available = n
	}
	v := h.Get("Rate-Limit-Reset")
	if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
		q.Reset = time.UnixMilli(ms)
	} else if ts, err := time.Parse(time.RFC3339, v); err == nil {
		q.Reset = ts
	}
	return q
}

// limitedUntil works out when a 429 clears. Running out of the day's quota lasts until its
// reset; the per-second limit shares the status but clears at once, and its reset header
// still names the end of the day, so the body's error code tells them apart.
func limitedUntil(resp *http.Response, q Quota) time.Time {
	var body struct {
		Fault struct {
			Detail struct {
				ErrorCode string `json:"errorcode"`
			} `json:"detail"`
		} `json:"fault"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body)
	if strings.Contains(strings.ToLower(body.Fault.Detail.ErrorCode), "spike") {
		return time.Now().Add(spikeWait)
	}
	if !q.Reset.IsZero() {
		return q.Reset
	}
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		return time.Now().Add(time.Duration(s) * time.Second)
	}
	return time.Time{}
}

func (t *Ticketmaster) Attractions(ctx context.Context, name string) ([]Attraction, error) {
	var out struct {
		Embedded struct {
			Attractions []tmAttraction `json:"attractions"`
		} `json:"_embedded"`
	}
	q := url.Values{"keyword": {name}, "classificationName": {"music"}, "size": {strconv.Itoa(attractionsPageSize)}}
	if err := t.get(ctx, "/attractions.json", q, &out); err != nil {
		return nil, err
	}
	list := make([]Attraction, 0, len(out.Embedded.Attractions))
	for _, a := range out.Embedded.Attractions {
		list = append(list, a.attraction())
	}
	return list, nil
}

func (t *Ticketmaster) ArtistShows(ctx context.Context, attractionID string) ([]Show, error) {
	shows, _, err := t.events(ctx, url.Values{"attractionId": {attractionID}, "classificationName": {"music"},
		"sort": {"date,asc"}, "size": {strconv.Itoa(artistShowsPageSize)}})
	return shows, err
}

func (t *Ticketmaster) Near(ctx context.Context, n NearQuery) ([]Show, int, error) {
	q := url.Values{"classificationName": {"music"}, "sort": {"date,asc"}, "size": {strconv.Itoa(n.Size)}, "page": {strconv.Itoa(n.Page)},
		"radius": {strconv.Itoa(n.Radius)}, "unit": {n.Unit}}
	switch {
	case n.GeoHash != "":
		q.Set("geoPoint", n.GeoHash)
	case n.PostalCode != "":
		q.Set("postalCode", n.PostalCode)
	default:
		q.Set("city", n.City)
	}
	if n.CountryCode != "" {
		q.Set("countryCode", n.CountryCode)
	}
	return t.events(ctx, q)
}

func (t *Ticketmaster) events(ctx context.Context, q url.Values) ([]Show, int, error) {
	var out struct {
		Embedded struct {
			Events []tmEvent `json:"events"`
		} `json:"_embedded"`
		Page struct {
			TotalPages int `json:"totalPages"`
		} `json:"page"`
	}
	if err := t.get(ctx, "/events.json", q, &out); err != nil {
		return nil, 0, err
	}
	shows := make([]Show, 0, len(out.Embedded.Events))
	for _, e := range out.Embedded.Events {
		if s, ok := e.show(); ok {
			shows = append(shows, s)
		}
	}
	return shows, out.Page.TotalPages, nil
}

// What the Discovery API sends; only the fields used. Numbers it sends as strings (venue
// coordinates) and fields whose documented type differs from its examples (localTime) are
// read loosely, so one odd value costs that value rather than the whole response.

type tmClassification struct {
	Primary bool   `json:"primary"`
	Segment tmName `json:"segment"`
	Genre   tmName `json:"genre"`
	SubGen  tmName `json:"subGenre"`
}

type tmName struct {
	Name string `json:"name"`
}

type tmAttraction struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Test            bool               `json:"test"`
	Classifications []tmClassification `json:"classifications"`
	ExternalLinks   struct {
		MusicBrainz []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"musicbrainz"`
	} `json:"externalLinks"`
	UpcomingEvents struct {
		Total *int `json:"_total"`
	} `json:"upcomingEvents"`
}

var tmUUID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func (a tmAttraction) attraction() Attraction {
	out := Attraction{ID: a.ID, Name: strings.TrimSpace(a.Name), Upcoming: -1}
	if a.UpcomingEvents.Total != nil {
		out.Upcoming = *a.UpcomingEvents.Total
	}
	// An act with no classifications at all isn't known to be anything else.
	out.Music = len(a.Classifications) == 0
	for _, c := range a.Classifications {
		if strings.EqualFold(c.Segment.Name, "Music") {
			out.Music = true
			if out.Genre == "" || c.Primary {
				out.Genre = usableGenre(c.Genre.Name)
			}
		}
	}
	for _, m := range a.ExternalLinks.MusicBrainz {
		id := tmUUID.FindString(m.ID)
		if id == "" {
			id = tmUUID.FindString(m.URL)
		}
		if id != "" {
			out.MBIDs = append(out.MBIDs, strings.ToLower(id))
		}
	}
	return out
}

// usableGenre drops the placeholder genres, which say nothing about the music.
func usableGenre(g string) string {
	switch strings.ToLower(strings.TrimSpace(g)) {
	case "", "undefined", "other", "music", "miscellaneous":
		return ""
	}
	return strings.TrimSpace(g)
}

type tmEvent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Test  bool   `json:"test"`
	Dates struct {
		Start struct {
			LocalDate string          `json:"localDate"`
			LocalTime json.RawMessage `json:"localTime"`
			DateTime  string          `json:"dateTime"`
			DateTBD   bool            `json:"dateTBD"`
			DateTBA   bool            `json:"dateTBA"`
			TimeTBA   bool            `json:"timeTBA"`
			NoTime    bool            `json:"noSpecificTime"`
		} `json:"start"`
		Timezone string `json:"timezone"`
		Status   struct {
			Code string `json:"code"`
		} `json:"status"`
	} `json:"dates"`
	Classifications []tmClassification `json:"classifications"`
	Embedded        struct {
		Venues      []tmVenue      `json:"venues"`
		Attractions []tmAttraction `json:"attractions"`
	} `json:"_embedded"`
}

type tmVenue struct {
	Name       string `json:"name"`
	PostalCode string `json:"postalCode"`
	Timezone   string `json:"timezone"`
	City       tmName `json:"city"`
	State      struct {
		StateCode string `json:"stateCode"`
	} `json:"state"`
	Country struct {
		CountryCode string `json:"countryCode"`
	} `json:"country"`
	Location struct {
		Latitude  json.RawMessage `json:"latitude"`
		Longitude json.RawMessage `json:"longitude"`
	} `json:"location"`
}

// looseFloat reads a number sent as a string or a number.
func looseFloat(raw json.RawMessage) (float64, bool) {
	v, err := strconv.ParseFloat(strings.Trim(string(raw), `"`), 64)
	return v, err == nil
}

// show converts an event, leaving out what isn't going ahead, has no date or can't be linked to.
func (e tmEvent) show() (Show, bool) {
	start := e.Dates.Start
	if e.Test || start.LocalDate == "" || start.DateTBD || start.DateTBA || !validLink(e.URL) {
		return Show{}, false
	}
	if _, err := time.Parse(time.DateOnly, start.LocalDate); err != nil {
		return Show{}, false
	}
	s := Show{ID: e.ID, Title: strings.TrimSpace(e.Name), URL: e.URL, Source: "ticketmaster", LocalDate: start.LocalDate, TimeZone: e.Dates.Timezone}
	switch strings.ToLower(e.Dates.Status.Code) {
	// Offsale isn't defined in the docs: it says tickets aren't being sold just now, not that
	// the show is off, so it stays. It can be sold out, or not on sale yet.
	case "canceled", "cancelled":
		return Show{}, false
	case "postponed":
		s.Status = "postponed"
	case "rescheduled":
		s.Status = "rescheduled"
	default:
		s.Status = "scheduled"
	}
	if !start.TimeTBA && !start.NoTime {
		var lt string
		if json.Unmarshal(start.LocalTime, &lt) == nil && len(lt) >= 5 {
			s.LocalTime = lt[:5]
		}
		if ts, err := time.Parse(time.RFC3339, start.DateTime); err == nil {
			s.StartsAt = ts.UTC()
		}
	}
	for _, c := range e.Classifications {
		if s.Genre == "" || c.Primary {
			s.Genre, s.SubGenre = usableGenre(c.Genre.Name), usableGenre(c.SubGen.Name)
		}
	}
	if len(e.Embedded.Venues) > 0 {
		v := e.Embedded.Venues[0]
		s.Venue = Venue{Name: strings.TrimSpace(v.Name), City: strings.TrimSpace(v.City.Name), Region: v.State.StateCode, Country: v.Country.CountryCode, Postal: v.PostalCode}
		lat, okLat := looseFloat(v.Location.Latitude)
		lon, okLon := looseFloat(v.Location.Longitude)
		if okLat && okLon && !(lat == 0 && lon == 0) {
			s.Venue.Lat, s.Venue.Lon, s.Venue.HasPoint = lat, lon, true
		}
		if s.TimeZone == "" {
			s.TimeZone = v.Timezone
		}
	}
	for _, a := range e.Embedded.Attractions {
		if !a.Test {
			s.Acts = append(s.Acts, a.attraction())
		}
	}
	return s, true
}

// validLink keeps tickets links to the web, so a bad value can't become a link that runs.
func validLink(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}
