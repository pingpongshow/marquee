// Package intown is In Town (MUSIC-27): concerts for the people on this server, from the
// admin's own Ticketmaster key. Everything fetched lives in a cache file of its own that can
// be deleted at any time; the main database holds only the on/off setting, the key and each
// person's location.
package intown

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Source is where shows come from. Ticketmaster is the only one so far.
type Source interface {
	// Name is the source's id in the data, e.g. ticketmaster.
	Name() string
	// Attractions searches acts by name. Results aren't in any useful order: tribute bands
	// can come before the act itself.
	Attractions(ctx context.Context, name string) ([]Attraction, error)
	// ArtistShows lists an attraction's upcoming shows.
	ArtistShows(ctx context.Context, attractionID string) ([]Show, error)
	// Near searches music shows by location, soonest first, one page at a time. It reports
	// how many pages there are.
	Near(ctx context.Context, q NearQuery) ([]Show, int, error)
}

// Attraction is an act as the source knows it.
type Attraction struct {
	ID, Name string
	MBIDs    []string
	Genre    string
	// Music is false for an act classified as something else (sport, theatre…).
	Music bool
	// Upcoming is the source's count of upcoming shows; -1 when it didn't say.
	Upcoming int
}

// Venue is where a show is. HasPoint says whether Lat and Lon are known.
type Venue struct {
	Name, City, Region, Country, Postal string
	Lat, Lon                            float64
	HasPoint                            bool
}

// Show is one event. Acts are everyone on the bill, headliner first.
type Show struct {
	ID, Title, URL string
	Source         string
	// LocalDate (YYYY-MM-DD) and LocalTime (HH:mm, or empty) are at the venue.
	LocalDate, LocalTime string
	TimeZone             string
	StartsAt             time.Time // zero when the time isn't announced
	Status               string    // scheduled, postponed, rescheduled
	Genre, SubGenre      string
	Venue                Venue
	Acts                 []Attraction
}

// Location is where someone is, coarsely: a postcode or a city, plus country, and how far
// they'd go. Never coordinates.
type Location struct {
	PostalCode, City, CountryCode string
	Distance                      int
	Unit                          string // miles or km
}

// NearQuery is one page of a search by location: by geohash when the location's point is
// known, otherwise by its postcode or city.
type NearQuery struct {
	PostalCode, City, CountryCode string
	GeoHash                       string
	Radius                        int
	Unit                          string
	Size, Page                    int
}

var (
	// ErrBadKey means the source rejected the key.
	ErrBadKey = errors.New("Ticketmaster rejected the key")
	// ErrUnreachable means the request didn't get an answer: the network, not this one artist.
	ErrUnreachable = errors.New("couldn't reach Ticketmaster")
	// ErrOverBudget means a call was held back to stay inside the day's budget, or while
	// the source has said to wait.
	ErrOverBudget = errors.New("over the day's budget")
)

// RateLimitError is the source saying to slow down. Until is when it said it would be ready,
// zero when it didn't say.
type RateLimitError struct{ Until time.Time }

func (e *RateLimitError) Error() string {
	if e.Until.IsZero() {
		return "Ticketmaster asked us to slow down"
	}
	return fmt.Sprintf("Ticketmaster's limit was reached until %s", e.Until.UTC().Format(time.RFC3339))
}

// Quota is what a response said about the key's quota. -1 means it didn't say.
type Quota struct {
	Limit, Available int
	Reset            time.Time
}

// Limiter is told about every call, before and after. Before holds a call to the pace and
// refuses it when over budget.
type Limiter interface {
	Before(ctx context.Context) error
	After(q Quota)
	// Limited records the source saying to wait.
	Limited(until time.Time)
	// Rejected records the source refusing the key.
	Rejected()
}
