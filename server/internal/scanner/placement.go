package scanner

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"marquee/internal/library"
	"marquee/internal/probe"
)

// Moving a track to another album or album artist without editing its file. The choice is
// kept per file (track_placements) and wins over the tags whenever the file is placed again;
// a scan otherwise never re-places a file it already knows. The track item itself moves, so
// its plays, ratings and favourites go with it.

var (
	ErrNotTrack          = errors.New("only tracks in a music library can be moved")
	ErrAlbumNotInLibrary = errors.New("that album isn't in this track's library")
	ErrNoAlbum           = errors.New("choose an album, or give an album artist and an album title")
)

// Placement is where an admin puts a track: AlbumID, an album in the same library, or an
// album by name (created if missing). Artist, when set, replaces the track's artist.
type Placement struct {
	AlbumID                    int64
	AlbumArtist, Album, Artist string
}

// override is a stored placement; the zero value is "as tagged".
type override struct {
	AlbumID                    int64
	AlbumArtist, Album, Artist string
}

func (o override) apply(t trackTags) trackTags {
	if o.Album == "" {
		return t
	}
	t.AlbumArtist, t.Album = o.AlbumArtist, o.Album
	if o.Artist != "" {
		t.Artist = o.Artist
	}
	// The tags' MusicBrainz ids belong to the album and artist they name.
	t.MBAlbumID, t.MBArtistID = "", ""
	return t
}

func (w *writer) placementOverride(ctx context.Context, path string) (override, error) {
	var o override
	var albumID sql.NullInt64
	var artist sql.NullString
	err := w.tx.QueryRowContext(ctx, `SELECT album_id, album_artist, album, artist FROM track_placements WHERE path = ?`, path).
		Scan(&albumID, &o.AlbumArtist, &o.Album, &artist)
	if err == sql.ErrNoRows {
		return override{}, nil
	}
	o.AlbumID, o.Artist = albumID.Int64, artist.String
	return o, err
}

// manualMBID is an admin's MusicBrainz match for the artist or album with this scan key.
func (w *writer) manualMBID(ctx context.Context, kind, key string) string {
	var v string
	w.tx.QueryRowContext(ctx, `SELECT mbid FROM music_matches WHERE library_id = ? AND kind = ? AND scan_key = ?`, w.lib.ID, kind, key).Scan(&v)
	return v
}

// PlaceTrack moves a track to another album, now and on later scans. Albums and artists
// left empty are removed the way a scan removes them. sql.ErrNoRows: no such item.
func PlaceTrack(ctx context.Context, db *sql.DB, trackID int64, p Placement) error {
	return moveTrack(ctx, db, trackID, &p)
}

// UnplaceTrack puts a moved track back where its file's tags say it belongs.
func UnplaceTrack(ctx context.Context, db *sql.DB, trackID int64) error {
	return moveTrack(ctx, db, trackID, nil)
}

// HasPlacement reports whether a track was moved by hand.
func HasPlacement(ctx context.Context, db *sql.DB, trackID int64) bool {
	var ok bool
	db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM track_placements p JOIN media_files f ON f.path = p.path
		JOIN media_versions v ON v.id = f.version_id WHERE v.item_id = ?)`, trackID).Scan(&ok)
	return ok
}

func moveTrack(ctx context.Context, db *sql.DB, trackID int64, p *Placement) error {
	var typ string
	var libID int64
	if err := db.QueryRowContext(ctx, `SELECT type, library_id FROM items WHERE id = ?`, trackID).Scan(&typ, &libID); err != nil {
		return err
	}
	lib, err := library.NewStore(db).Get(ctx, libID)
	if err != nil {
		return err
	}
	if typ != "track" || lib.Type != library.Music {
		return ErrNotTrack
	}
	var paths []string
	var raw string
	rows, err := db.QueryContext(ctx, `SELECT f.path, COALESCE(f.probe_json, '') FROM media_files f JOIN media_versions v ON v.id = f.version_id
		WHERE v.item_id = ? ORDER BY f.id`, trackID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var path, pj string
		if err := rows.Scan(&path, &pj); err != nil {
			rows.Close()
			return err
		}
		if len(paths) == 0 {
			raw = pj
		}
		paths = append(paths, path)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return ErrNotTrack
	}

	// The file as a scan saw it: its tags and streams (kept from the probe), and its folder.
	c := candidate{Path: paths[0], Rel: filepath.Base(paths[0])}
	for _, r := range lib.Paths {
		r = filepath.Clean(r)
		if strings.HasPrefix(c.Path, r+string(filepath.Separator)) && len(r) > len(c.Root) {
			rel, _ := filepath.Rel(r, c.Path)
			c.Root, c.Rel = r, filepath.ToSlash(rel)
		}
	}
	res := &probe.Result{}
	if raw != "" {
		if r, err := probe.Parse([]byte(raw)); err == nil {
			res = r
		}
	}
	dir := filepath.Dir(c.Path)
	var names []string
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	w := &writer{db: db, tx: tx, lib: lib, dirs: map[string][]string{dir: names}, keyCache: map[string]int64{}}

	var o override
	if p != nil {
		o = override{AlbumID: p.AlbumID, AlbumArtist: strings.TrimSpace(p.AlbumArtist), Album: strings.TrimSpace(p.Album), Artist: strings.TrimSpace(p.Artist)}
		if p.AlbumID > 0 {
			var albumLib int64
			var albumType string
			err := tx.QueryRowContext(ctx, `SELECT a.library_id, a.type, a.title, COALESCE(NULLIF(a.artist_credit, ''), ar.title, '')
				FROM items a LEFT JOIN items ar ON ar.id = a.parent_id WHERE a.id = ?`, p.AlbumID).Scan(&albumLib, &albumType, &o.Album, &o.AlbumArtist)
			if err == sql.ErrNoRows || (err == nil && (albumLib != libID || albumType != "album")) {
				return ErrAlbumNotInLibrary
			}
			if err != nil {
				return err
			}
		} else if o.AlbumArtist == "" || o.Album == "" {
			return ErrNoAlbum
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for _, path := range paths {
			if _, err := tx.ExecContext(ctx, `INSERT INTO track_placements (path, library_id, album_id, album_artist, album, artist, placed_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (path) DO UPDATE SET library_id = excluded.library_id, album_id = excluded.album_id, album_artist = excluded.album_artist,
					album = excluded.album, artist = excluded.artist, placed_at = excluded.placed_at`,
				path, libID, nullInt64(o.AlbumID), o.AlbumArtist, o.Album, nullStr(o.Artist), now); err != nil {
				return err
			}
		}
	} else {
		for _, path := range paths {
			if _, err := tx.ExecContext(ctx, `DELETE FROM track_placements WHERE path = ?`, path); err != nil {
				return err
			}
		}
	}

	t := o.apply(readTrackTags(res.Tags, c.Rel))
	artistID, albumID, err := w.trackParents(ctx, c, res, t, o.AlbumID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE items SET parent_id = ?, grandparent_id = ? WHERE id = ?`, albumID, artistID, trackID); err != nil {
		return err
	}
	// The track's own artist changes only when asked to, or back to the tags.
	if p == nil || o.Artist != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE items SET artist_credit = ? WHERE id = ?`, nullStr(t.Artist), trackID); err != nil {
			return err
		}
	}
	// Popular tracks belong to their artist.
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_popular WHERE track_id = ? AND artist_id <> ?`, trackID, artistID); err != nil {
		return err
	}
	if err := tidy(ctx, tx, libID); err != nil {
		return err
	}
	return tx.Commit()
}
