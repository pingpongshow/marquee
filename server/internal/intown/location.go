package intown

import (
	"errors"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Defaults for someone's distance: 50 miles, or about the same in km.
const (
	defaultMiles = 50
	defaultKm    = 80
	maxDistance  = 500
)

var (
	validCountry = regexp.MustCompile(`^[A-Z]{2}$`)
	validPostal  = regexp.MustCompile(`^[A-Z0-9][A-Z0-9 -]{0,11}$`)
)

// Clean checks and tidies a location, filling in the distance and unit. It keeps a postcode or
// a city, never both, and only the part of a postcode that names an area.
func Clean(l Location) (Location, error) {
	l.CountryCode = strings.ToUpper(strings.TrimSpace(l.CountryCode))
	l.PostalCode = strings.ToUpper(strings.Join(strings.Fields(l.PostalCode), " "))
	l.City = strings.Join(strings.Fields(l.City), " ")
	if !validCountry.MatchString(l.CountryCode) {
		return l, errors.New("choose a country")
	}
	if l.PostalCode == "" && l.City == "" {
		return l, errors.New("enter a postcode or a city")
	}
	if l.PostalCode != "" && l.City != "" {
		return l, errors.New("enter a postcode or a city, not both")
	}
	if l.PostalCode != "" && !validPostal.MatchString(l.PostalCode) {
		return l, errors.New("that doesn't look like a postcode")
	}
	l.PostalCode = coarsePostal(l.CountryCode, l.PostalCode)
	if len([]rune(l.City)) > 80 || strings.IndexFunc(l.City, unicode.IsControl) >= 0 {
		return l, errors.New("that doesn't look like a city")
	}
	switch l.Unit {
	case "":
		l.Unit = DefaultUnit(l.CountryCode)
	case "miles", "km":
	default:
		return l, errors.New("the distance must be in miles or km")
	}
	if l.Distance == 0 {
		l.Distance = defaultMiles
		if l.Unit == "km" {
			l.Distance = defaultKm
		}
	}
	if l.Distance < 1 || l.Distance > maxDistance {
		return l, errors.New("the distance must be between 1 and 500")
	}
	return l, nil
}

// coarsePostal cuts a postcode to the part that names an area, where the whole one comes close
// to naming a home: a UK postcode covers about 15 addresses, and an Eircode just one.
func coarsePostal(country, code string) string {
	whole := strings.NewReplacer(" ", "", "-", "").Replace(code)
	first := func(n int) string { return whole[:min(n, len(whole))] }
	switch country {
	case "GB":
		// The second half is always three characters, and the first half varies.
		if head, _, ok := strings.Cut(code, " "); ok {
			return head
		}
		if len(whole) >= 5 {
			return whole[:len(whole)-3]
		}
		return whole
	case "IE", "CA":
		return first(3)
	case "NL":
		return first(4)
	case "US":
		return first(5)
	}
	return code
}

// DefaultUnit is what a country's road signs use.
func DefaultUnit(country string) string {
	if country == "GB" || country == "US" {
		return "miles"
	}
	return "km"
}

// placeKey names a postcode or city. The postcode wins, being the more exact.
func placeKey(l Location) string {
	if l.PostalCode != "" {
		return l.CountryCode + "|p:" + l.PostalCode
	}
	return l.CountryCode + "|c:" + normalizeName(l.City)
}

// radiusSteps round searches up, so people nearby with similar distances share one.
var radiusSteps = []int{10, 25, 50, 75, 100, 150, 200, 300, 500}

func roundRadius(n int) int {
	for _, s := range radiusSteps {
		if n <= s {
			return s
		}
	}
	return radiusSteps[len(radiusSteps)-1]
}

func toMiles(d float64, unit string) float64 {
	if unit == "km" {
		return d / 1.609344
	}
	return d
}

func milesTo(d float64, unit string) float64 {
	if unit == "km" {
		return d * 1.609344
	}
	return d
}

// distanceMiles is the great-circle distance between two points.
func distanceMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthMiles = 3958.8
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthMiles * math.Asin(math.Min(1, math.Sqrt(a)))
}

// geoHashPrecision is a cell about 5 km across: rounded enough for neighbours to share a search.
const geoHashPrecision = 5

const geoHashBase32 = "0123456789bcdefghjkmnpqrstuvwxyz"

// geoHash encodes a point, which is how the source's geoPoint wants it.
func geoHash(lat, lon float64, precision int) string {
	latR, lonR := [2]float64{-90, 90}, [2]float64{-180, 180}
	var b strings.Builder
	bit, ch, even := 0, 0, true
	for b.Len() < precision {
		r, v := &latR, lat
		if even {
			r, v = &lonR, lon
		}
		mid := (r[0] + r[1]) / 2
		ch <<= 1
		if v >= mid {
			ch |= 1
			r[0] = mid
		} else {
			r[1] = mid
		}
		even = !even
		if bit++; bit == 5 {
			b.WriteByte(geoHashBase32[ch])
			bit, ch = 0, 0
		}
	}
	return b.String()
}

// median is the middle value, which a stray venue can't pull off course.
func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	if n := len(s); n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[len(s)/2]
}

// pointOf is the middle of the shows' venues; with city set, only those in that city count.
func pointOf(shows []Show, city string) (float64, float64, bool) {
	want := normalizeName(city)
	var lats, lons []float64
	for _, s := range shows {
		if s.Venue.HasPoint && (city == "" || normalizeName(s.Venue.City) == want) {
			lats, lons = append(lats, s.Venue.Lat), append(lons, s.Venue.Lon)
		}
	}
	if len(lats) == 0 {
		return 0, 0, false
	}
	return median(lats), median(lons), true
}
