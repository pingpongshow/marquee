package metadata

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"marquee/internal/metadata/deezer"
	"marquee/internal/metadata/musicbrainz"
	"marquee/internal/scanner/naming"
)

// Fix match for music: an admin picks the MusicBrainz artist or album (release group) an
// artist or album item is. The choice is kept in music_matches, keyed like the scanner keys
// the item, so it outlasts rescans and wins over MBIDs in tags; the details the old match
// brought (bio, type, country, years, styles, artist genres, release type, date and label,
// popular tracks) are replaced at once, except fields an admin locked.

var ErrNotMusicMatchable = errors.New("only artists and albums in a music library can be matched to MusicBrainz")

// MusicCandidate is a MusicBrainz artist or release group offered by Fix match.
type MusicCandidate struct {
	MBID, Name, Disambiguation string
	Type                       string // artists: person, group…; albums: album, ep, single, live…
	Country                    string
	Artist                     string // albums: the artist credit
	Year, BeginYear, EndYear   int
	TrackCount, Score          int
	Current                    bool
}

type musicItem struct {
	id, libID                                      int64
	typ, title, artist, scanKey, locked, sortTitle string
	released                                       string
	year                                           int
	current                                        string // the MBID it is matched to
}

func (s *Service) musicItem(ctx context.Context, itemID int64) (musicItem, error) {
	var it musicItem
	var libType string
	err := s.DB.QueryRowContext(ctx, `SELECT i.id, i.library_id, i.type, i.title, COALESCE(a.title, ''), COALESCE(i.scan_key, ''), i.locked_fields,
			i.sort_title, COALESCE(i.originally_available_at, ''), COALESCE(i.year, 0), l.type,
			COALESCE(mm.mbid, (SELECT value FROM external_ids x WHERE x.item_id = i.id
				AND x.provider = CASE i.type WHEN 'artist' THEN 'musicbrainz' ELSE 'musicbrainz_releasegroup' END), '')
		FROM items i JOIN libraries l ON l.id = i.library_id
		LEFT JOIN items a ON a.id = i.parent_id
		LEFT JOIN music_matches mm ON mm.library_id = i.library_id AND mm.kind = i.type AND mm.scan_key = i.scan_key
		WHERE i.id = ?`, itemID).Scan(&it.id, &it.libID, &it.typ, &it.title, &it.artist, &it.scanKey, &it.locked,
		&it.sortTitle, &it.released, &it.year, &libType, &it.current)
	if err != nil {
		return it, err
	}
	if libType != "music" || (it.typ != "artist" && it.typ != "album") {
		return it, ErrNotMusicMatchable
	}
	it.current = firstMBID(it.current)
	return it, nil
}

func (s *Service) musicBrainz() *musicbrainz.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mb == nil {
		s.mb = musicbrainz.New(MBUserAgent)
	}
	return s.mb
}

var mbLinkRe = regexp.MustCompile(`(?i)musicbrainz\.org/(artist|release-group|release)/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// parseMBRef recognises a musicbrainz.org link (kind: artist, release-group or release) or
// a bare MBID (kind ""). ok is false for anything else: a name to search for.
func parseMBRef(q string) (kind, mbid string, ok bool) {
	q = strings.TrimSpace(q)
	if m := mbLinkRe.FindStringSubmatch(q); m != nil {
		return strings.ToLower(m[1]), strings.ToLower(m[2]), true
	}
	if id := mbidPattern.FindString(q); id != "" && id == q {
		return "", strings.ToLower(id), true
	}
	return "", "", false
}

// MusicCandidates searches MusicBrainz for Fix match: artists by name (q, default the
// artist's name), albums by title (q, default the album's) and artist (default the album's
// artist). A MusicBrainz link or MBID in q is looked up directly.
func (s *Service) MusicCandidates(ctx context.Context, itemID int64, q, artist string) ([]MusicCandidate, error) {
	it, err := s.musicItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if q = strings.TrimSpace(q); q == "" {
		q = it.title
	}
	if artist = strings.TrimSpace(artist); artist == "" && it.typ == "album" {
		artist = it.artist
	}
	mb := s.musicBrainz()
	out := []MusicCandidate{}
	if kind, mbid, ok := parseMBRef(q); ok {
		if it.typ == "artist" {
			a, err := lookupArtist(ctx, mb, kind, mbid)
			if err != nil || a == nil {
				return out, err
			}
			return append(out, artistCandidate(*a, it.current)), nil
		}
		g, _, err := lookupReleaseGroup(ctx, mb, kind, mbid)
		if err != nil || g == nil {
			return out, err
		}
		return append(out, albumCandidate(*g, it.current)), nil
	}
	if it.typ == "artist" {
		res, err := mb.SearchArtists(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, a := range res {
			out = append(out, artistCandidate(a, it.current))
		}
		return out, nil
	}
	res, err := mb.SearchReleaseGroups(ctx, artist, q)
	if err != nil {
		return nil, err
	}
	for _, g := range res {
		out = append(out, albumCandidate(g, it.current))
	}
	return out, nil
}

func artistCandidate(a musicbrainz.Artist, current string) MusicCandidate {
	c := MusicCandidate{MBID: a.ID, Name: a.Name, Disambiguation: a.Disambiguation, Type: strings.ToLower(a.Type),
		Country: a.CountryCode(), Score: a.Score, Current: a.ID == current}
	c.BeginYear, c.EndYear = a.Years()
	return c
}

func albumCandidate(g musicbrainz.ReleaseGroup, current string) MusicCandidate {
	return MusicCandidate{MBID: g.ID, Name: g.Title, Disambiguation: g.Disambiguation, Type: g.ReleaseType(),
		Artist: g.Credit(), Year: year(g.FirstReleaseDate), Score: g.Score, Current: g.ID == current}
}

// lookupArtist fetches an artist by MBID (nil when MusicBrainz has none).
func lookupArtist(ctx context.Context, mb *musicbrainz.Client, kind, mbid string) (*musicbrainz.Artist, error) {
	if kind != "" && kind != "artist" {
		return nil, MatchRefError("that link is for an album, but this is an artist")
	}
	a, err := mb.Artist(ctx, mbid)
	if errors.Is(err, musicbrainz.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// lookupReleaseGroup fetches a release group by its MBID or one of its releases' (nil when
// MusicBrainz has neither), and the release's label when a release was given.
func lookupReleaseGroup(ctx context.Context, mb *musicbrainz.Client, kind, mbid string) (*musicbrainz.ReleaseGroup, string, error) {
	if kind == "artist" {
		return nil, "", MatchRefError("that link is for an artist, but this is an album")
	}
	label := ""
	if kind != "release" {
		g, err := mb.ReleaseGroup(ctx, mbid)
		if err == nil {
			return &g, "", nil
		}
		if !errors.Is(err, musicbrainz.ErrNotFound) || kind == "release-group" {
			return nil, "", ignoreNotFound(err)
		}
	}
	// A release (an album's MBID in tags): its release group.
	id, l, err := mb.ReleaseGroupOf(ctx, mbid)
	if err != nil {
		return nil, "", ignoreNotFound(err)
	}
	label = l
	g, err := mb.ReleaseGroup(ctx, id)
	if err != nil {
		return nil, "", ignoreNotFound(err)
	}
	return &g, label, nil
}

func ignoreNotFound(err error) error {
	if errors.Is(err, musicbrainz.ErrNotFound) {
		return nil
	}
	return err
}

// MusicMatchTo matches an artist to a MusicBrainz artist, or an album to a release group (a
// release's MBID or link names its group), and looks its details up again right away.
func (s *Service) MusicMatchTo(ctx context.Context, itemID int64, ref string) error {
	it, err := s.musicItem(ctx, itemID)
	if err != nil {
		return err
	}
	kind, mbid, ok := parseMBRef(ref)
	if !ok {
		return MatchRefError("that isn't a MusicBrainz id")
	}
	mb := s.musicBrainz()
	var a *musicbrainz.Artist
	var g *musicbrainz.ReleaseGroup
	label := ""
	if it.typ == "artist" {
		if a, err = lookupArtist(ctx, mb, kind, mbid); err == nil && a == nil {
			err = MatchRefError("MusicBrainz has no artist with that id")
		}
		if err != nil {
			return err
		}
		mbid = a.ID
	} else {
		if g, label, err = lookupReleaseGroup(ctx, mb, kind, mbid); err == nil && g == nil {
			err = MatchRefError("MusicBrainz has no album with that id")
		}
		if err != nil {
			return err
		}
		if label == "" {
			if label, err = mb.ReleaseGroupLabel(ctx, g.ID); err != nil && !errors.Is(err, musicbrainz.ErrNotFound) {
				return err
			}
		}
		mbid = g.ID
	}

	// Forget the old match and what it brought.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, q := range []string{
		`DELETE FROM external_ids WHERE item_id = ? AND provider IN ('musicbrainz', 'musicbrainz_releasegroup')`,
		`DELETE FROM musicbrainz_lookups WHERE item_id = ?`,
		`DELETE FROM music_info WHERE item_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, it.id); err != nil {
			return err
		}
	}
	if it.scanKey != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_matches (library_id, kind, scan_key, mbid, matched_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (library_id, kind, scan_key) DO UPDATE SET mbid = excluded.mbid, matched_at = excluded.matched_at`,
			it.libID, it.typ, it.scanKey, mbid, now); err != nil {
			return err
		}
	}
	clear := func(q string, fields ...string) error {
		if len(fields) > 0 && lockedIn(it.locked, fields...) {
			return nil
		}
		_, err := tx.ExecContext(ctx, q, it.id)
		return err
	}
	clearStyles := `DELETE FROM item_tags WHERE item_id = ? AND tag_id IN (SELECT id FROM tags WHERE kind = 'style')`
	if err := clear(clearStyles, "genres", "styles"); err != nil {
		return err
	}
	if it.typ == "artist" {
		// An artist's genres, bio and popular tracks come only from MusicBrainz and the sites
		// it links to; the sort name may have come from it too.
		if err := clear(`DELETE FROM item_tags WHERE item_id = ? AND tag_id IN (SELECT id FROM tags WHERE kind = 'genre')`, "genres"); err != nil {
			return err
		}
		if err := clear(`UPDATE items SET summary = NULL WHERE id = ?`, "summary"); err != nil {
			return err
		}
		if err := clear(`DELETE FROM music_popular WHERE artist_id = ?`); err != nil {
			return err
		}
		if !lockedIn(it.locked, "sortTitle", "sort_title", "title") {
			it.sortTitle = naming.SortTitle(it.title)
			if _, err := tx.ExecContext(ctx, `UPDATE items SET sort_title = ? WHERE id = ?`, it.sortTitle, it.id); err != nil {
				return err
			}
		}
	} else if err := clear(`UPDATE items SET release_type = NULL WHERE id = ?`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	t := mbTodo{id: it.id, typ: it.typ, title: it.title, artist: it.artist, locked: it.locked, sortTitle: it.sortTitle,
		released: it.released, year: it.year, manual: true}
	if a != nil {
		t.mbid = a.ID
		err = s.applyArtist(ctx, t, *a)
	} else {
		t.rgid = g.ID
		err = s.applyAlbum(ctx, t, *g, label)
	}
	if err != nil {
		return err
	}
	s.DB.ExecContext(ctx, `INSERT INTO musicbrainz_lookups (item_id, checked_at, found) VALUES (?, ?, 1)
		ON CONFLICT (item_id) DO UPDATE SET checked_at = excluded.checked_at, found = 1`, it.id, now)
	if s.AfterMusicDetails != nil {
		s.AfterMusicDetails()
	}
	return nil
}

// artistPicture replaces a Deezer artist photo found by name with the one MusicBrainz links
// the artist to, when it links one (two artists may share a name; a match chosen by hand is
// the right one).
func (s *Service) artistPicture(ctx context.Context, t mbTodo, a musicbrainz.Artist) {
	id := a.DeezerID()
	if id == "" {
		return
	}
	s.mu.Lock()
	if s.deezer == nil {
		s.deezer = deezer.New()
	}
	dz := s.deezer
	s.mu.Unlock()
	d, err := dz.Artist(ctx, id)
	if err != nil || isPlaceholder(d.PictureXL) {
		return
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `DELETE FROM artwork WHERE item_id = ? AND kind = 'poster' AND source = 'deezer'`, t.id)
	tx.ExecContext(ctx, `INSERT INTO artwork(item_id, kind, source, remote_url, selected)
		SELECT ?, 'poster', 'deezer', ?, NOT EXISTS (SELECT 1 FROM artwork WHERE item_id = ? AND kind = 'poster' AND selected = 1)`,
		t.id, d.PictureXL, t.id)
	tx.Commit()
}
