package items

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// In Town (MUSIC-27): which artists are in the libraries, and how much someone likes them.

// MusicArtist is an artist in a music library.
type MusicArtist struct {
	ID, LibraryID int64
	Name          string
	// MBID is their MusicBrainz id, "" when unknown: an admin's Fix match, else from tags or
	// a lookup.
	MBID string
	// Cared: someone on this server favourited, rated or played them or their music.
	Cared bool
}

// notAnArtist are the placeholder names the scanner files music under.
const notAnArtist = `('Unknown Artist', 'Various Artists', '')`

var artistMBID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// MusicArtists lists every artist in the music libraries, for anyone: use VisibleMusicArtists
// to narrow it to what someone may see.
func (s *Store) MusicArtists(ctx context.Context) ([]MusicArtist, error) {
	cared := map[int64]bool{}
	err := eachRow(ctx, s.db, `SELECT DISTINCT CASE i.type WHEN 'artist' THEN i.id WHEN 'album' THEN i.parent_id ELSE i.grandparent_id END
		FROM user_item_state u JOIN items i ON i.id = u.item_id AND i.type IN ('artist', 'album', 'track')
		WHERE u.favorite_at IS NOT NULL OR u.rating IS NOT NULL OR u.play_count > 0`, nil, func(r *sql.Rows) error {
		var id sql.NullInt64
		if err := r.Scan(&id); err != nil {
			return err
		}
		cared[id.Int64] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []MusicArtist
	err = eachRow(ctx, s.db, `SELECT i.id, i.library_id, i.title, COALESCE(mm.mbid, ''), COALESCE(x.value, '')
		FROM items i JOIN libraries l ON l.id = i.library_id AND l.type = 'music'
		LEFT JOIN external_ids x ON x.item_id = i.id AND x.provider = 'musicbrainz'
		LEFT JOIN music_matches mm ON mm.library_id = i.library_id AND mm.kind = 'artist' AND mm.scan_key = i.scan_key
		WHERE i.type = 'artist' AND i.title NOT IN `+notAnArtist+` ORDER BY i.id`, nil, func(r *sql.Rows) error {
		var a MusicArtist
		var manual, tagged string
		if err := r.Scan(&a.ID, &a.LibraryID, &a.Name, &manual, &tagged); err != nil {
			return err
		}
		a.MBID = chooseMBID(manual, tagged)
		a.Cared = cared[a.ID]
		out = append(out, a)
		return nil
	})
	return out, err
}

// MusicArtistByID is one artist, without Cared; ok is false for anything else.
func (s *Store) MusicArtistByID(ctx context.Context, id int64) (a MusicArtist, ok bool, err error) {
	var manual, tagged string
	err = s.db.QueryRowContext(ctx, `SELECT i.id, i.library_id, i.title, COALESCE(mm.mbid, ''), COALESCE(x.value, '')
		FROM items i JOIN libraries l ON l.id = i.library_id AND l.type = 'music'
		LEFT JOIN external_ids x ON x.item_id = i.id AND x.provider = 'musicbrainz'
		LEFT JOIN music_matches mm ON mm.library_id = i.library_id AND mm.kind = 'artist' AND mm.scan_key = i.scan_key
		WHERE i.id = ? AND i.type = 'artist' AND i.title NOT IN `+notAnArtist, id).Scan(&a.ID, &a.LibraryID, &a.Name, &manual, &tagged)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, err
	}
	a.MBID = chooseMBID(manual, tagged)
	return a, true, nil
}

// chooseMBID is an admin's match, else the id from tags or a lookup. Tags of tracks by several
// artists hold several ids; the first is this one's.
func chooseMBID(manual, tagged string) string {
	if id := artistMBID.FindString(manual); id != "" {
		return strings.ToLower(id)
	}
	return strings.ToLower(artistMBID.FindString(tagged))
}

// VisibleMusicArtists is the set of artists acc can see.
func (s *Store) VisibleMusicArtists(ctx context.Context, acc Access) (map[int64]bool, error) {
	ac, aargs := acc.clause()
	out := map[int64]bool{}
	err := eachRow(ctx, s.db, `SELECT i.id`+summaryFrom+` WHERE i.type = 'artist' AND i.library_id IN (SELECT id FROM libraries WHERE type = 'music') AND `+ac, aargs, func(r *sql.Rows) error {
		var id int64
		if err := r.Scan(&id); err != nil {
			return err
		}
		out[id] = true
		return nil
	})
	return out, err
}

// ArtistLiking is what someone's marks and plays say about an artist.
type ArtistLiking struct {
	Favorite bool
	Rating   float64 // their own rating of the artist, 0–10; 0 = none
	// FavoriteParts are favourite albums and songs; HighRated those rated 3.5 stars or more.
	FavoriteParts, HighRated int
	// RatedParts and RatedSum are all their ratings of the artist's albums and songs.
	RatedParts int
	RatedSum   float64
	Plays      int
	// Thumbs are songs given a thumbs down; Touched are songs marked or played at all; Songs
	// is every song by the artist, counted only when there are thumbs down to weigh.
	Thumbs, Touched, Songs int
	Genres                 []string
}

// ArtistLikings gathers uid's liking of the given artists, leaving out the rest: their plays
// of an artist they may no longer see say nothing here.
func (s *Store) ArtistLikings(ctx context.Context, uid int64, artists map[int64]bool) (map[int64]*ArtistLiking, error) {
	out := map[int64]*ArtistLiking{}
	of := func(id int64) *ArtistLiking {
		if !artists[id] {
			return nil
		}
		if out[id] == nil {
			out[id] = &ArtistLiking{}
		}
		return out[id]
	}
	thumbs := false
	err := eachRow(ctx, s.db, `SELECT i.type, CASE i.type WHEN 'artist' THEN i.id WHEN 'album' THEN i.parent_id ELSE i.grandparent_id END,
			u.favorite_at IS NOT NULL, COALESCE(u.rating, 0), COALESCE(u.play_count, 0), u.disliked_at IS NOT NULL
		FROM user_item_state u JOIN items i ON i.id = u.item_id AND i.type IN ('artist', 'album', 'track') WHERE u.user_id = ?`, []any{uid}, func(r *sql.Rows) error {
		var typ string
		var artist sql.NullInt64
		var fav, disliked bool
		var rating float64
		var plays int
		if err := r.Scan(&typ, &artist, &fav, &rating, &plays, &disliked); err != nil {
			return err
		}
		l := of(artist.Int64)
		if l == nil {
			return nil
		}
		if typ == "artist" {
			l.Favorite, l.Rating = fav, rating
			return nil
		}
		if fav {
			l.FavoriteParts++
		}
		if rating >= topRating {
			l.HighRated++
		}
		if rating > 0 {
			l.RatedParts++
			l.RatedSum += rating
		}
		if typ == "track" {
			l.Plays += plays
			if disliked {
				l.Thumbs++
				thumbs = true
			}
			if fav || disliked || rating > 0 || plays > 0 {
				l.Touched++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if thumbs {
		err = eachRow(ctx, s.db, `SELECT grandparent_id, COUNT(*) FROM items WHERE type = 'track' AND grandparent_id IS NOT NULL GROUP BY grandparent_id`, nil, func(r *sql.Rows) error {
			var id int64
			var n int
			if err := r.Scan(&id, &n); err != nil {
				return err
			}
			if l := out[id]; l != nil {
				l.Songs = n
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	// Genres on the artist and on their albums, for the artists that say something about
	// this person's taste.
	err = eachRow(ctx, s.db, `SELECT DISTINCT CASE i.type WHEN 'artist' THEN i.id ELSE i.parent_id END, t.name
		FROM item_tags x JOIN tags t ON t.id = x.tag_id AND t.kind = 'genre' JOIN items i ON i.id = x.item_id AND i.type IN ('artist', 'album')`, nil, func(r *sql.Rows) error {
		var id sql.NullInt64
		var name string
		if err := r.Scan(&id, &name); err != nil {
			return err
		}
		if l := out[id.Int64]; l != nil {
			l.Genres = append(l.Genres, name)
		}
		return nil
	})
	return out, err
}
