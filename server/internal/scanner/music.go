package scanner

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Artist names that legitimately contain commas and must not be split.
var commaArtists = func() []*regexp.Regexp {
	names := []string{
		"Tyler, The Creator", "Earth, Wind & Fire", "Crosby, Stills, Nash & Young", "Crosby, Stills & Nash",
		"Emerson, Lake & Palmer", "Peter, Paul and Mary", "Blood, Sweat & Tears", "Lambert, Hendricks & Ross",
		"Now, Now", "Me, Myself and I", "Hello, Dolly",
	}
	out := make([]*regexp.Regexp, len(names))
	for i, n := range names {
		out[i] = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(n))
	}
	return out
}()

// splitArtists splits a comma- or semicolon-joined credit into individual artists, e.g.
// "&ME,Rampa,Adam Port,Cubicolor" → [&ME Rampa Adam Port Cubicolor]. "/" and "&" are not
// separators ("AC/DC", "Simon & Garfunkel").
func splitArtists(credit string) []string {
	credit = strings.TrimSpace(credit)
	if credit == "" {
		return nil
	}
	// Mask known comma names so they survive the split.
	var masked []string
	for _, re := range commaArtists {
		credit = re.ReplaceAllStringFunc(credit, func(m string) string {
			masked = append(masked, m)
			return "\x00" + strconv.Itoa(len(masked)-1) + "\x00"
		})
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range splitCredit(credit) {
		for i, m := range masked {
			p = strings.ReplaceAll(p, "\x00"+strconv.Itoa(i)+"\x00", m)
		}
		p = strings.TrimSpace(p)
		if p != "" && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	return out
}

// splitCredit splits on ',' and ';', except a comma between digits ("10,000 Maniacs").
func splitCredit(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ';' || (c == ',' && !(i > 0 && i+1 < len(s) && isDigit(s[i-1]) && isDigit(s[i+1]))) {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

type trackTags struct {
	AlbumArtist, Artist, Album, Title string
	Track, Disc, Year                 int
	MBAlbumID, MBArtistID, MBTrackID  string
	Genre                             string
}

func firstTag(tags map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(tags[k]); v != "" {
			return v
		}
	}
	return ""
}

// leadingInt parses "3", "3/12", "03 of 12".
func leadingInt(s string) int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// readTrackTags reads music tags, falling back to Artist/Album/NN Title.ext from the path.
func readTrackTags(tags map[string]string, rel string) trackTags {
	t := trackTags{
		Artist:      firstTag(tags, "artist", "artists"),
		AlbumArtist: firstTag(tags, "album_artist", "albumartist", "album artist"),
		Album:       firstTag(tags, "album"),
		Title:       firstTag(tags, "title"),
		Track:       leadingInt(firstTag(tags, "track", "tracknumber")),
		Disc:        leadingInt(firstTag(tags, "disc", "discnumber")),
		Year:        leadingInt(firstTag(tags, "date", "originaldate", "year", "original_year")),
		MBAlbumID:   firstTag(tags, "musicbrainz_albumid", "musicbrainz album id"),
		MBArtistID:  firstTag(tags, "musicbrainz_albumartistid", "musicbrainz album artist id", "musicbrainz_artistid"),
		MBTrackID:   firstTag(tags, "musicbrainz_trackid", "musicbrainz_releasetrackid"),
		Genre:       firstTag(tags, "genre"),
	}
	if t.Year < 1000 || t.Year > 9999 {
		t.Year = 0
	}
	parts := strings.Split(rel, "/")
	file := strings.TrimSuffix(parts[len(parts)-1], path.Ext(parts[len(parts)-1]))
	if t.AlbumArtist == "" {
		t.AlbumArtist = t.Artist
	}
	if t.AlbumArtist == "" && len(parts) >= 3 {
		t.AlbumArtist = parts[len(parts)-3]
	}
	if t.Artist == "" {
		t.Artist = t.AlbumArtist
	}
	if t.Album == "" && len(parts) >= 2 {
		t.Album = parts[len(parts)-2]
	}
	if t.Title == "" {
		t.Title = strings.TrimLeft(strings.TrimLeft(file, "0123456789"), " .-_")
		if t.Track == 0 {
			t.Track = leadingInt(file)
		}
		if t.Title == "" {
			t.Title = file
		}
	}
	if t.AlbumArtist == "" {
		t.AlbumArtist = "Unknown Artist"
	}
	if t.Artist == "" {
		t.Artist = t.AlbumArtist
	}
	if t.Album == "" {
		t.Album = "Unknown Album"
	}
	return t
}
