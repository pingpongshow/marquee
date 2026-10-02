package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"marquee/internal/library"
	"marquee/internal/probe"
	"marquee/internal/scanner/naming"
)

// placement is where a new file belongs in the item tree.
type placement struct {
	ItemID int64
	Label  string // version label (edition)
	Part   int    // stacked part number, 0 if not a multi-part file
}

type itemFields struct {
	Idx, AbsIdx, Disc int
	Title, Original   string
	Year              int
	Date              string // YYYY-MM-DD
	ArtistCredit      string
	MatchState        string
}

// Folders and filename suffixes (Plex/Jellyfin conventions) that hold extras rather than
// main content (LIB-8). "skip" marks samples and subtitle folders, which aren't extras.
var extrasDirs = map[string]string{"extras": "other", "featurettes": "featurette", "behind the scenes": "behind_the_scenes",
	"deleted scenes": "deleted_scene", "interviews": "interview", "scenes": "scene", "shorts": "short", "trailers": "trailer",
	"other": "other", "bonus": "other", "bonus features": "other", "samples": "skip", "sample": "skip", "subs": "skip", "subtitles": "skip"}

var extrasSuffixes = [][2]string{{"-trailer", "trailer"}, {"-featurette", "featurette"}, {"-behindthescenes", "behind_the_scenes"},
	{"-deleted", "deleted_scene"}, {"-interview", "interview"}, {"-scene", "scene"}, {"-short", "short"}, {"-other", "other"}, {"-sample", "skip"}}

// extraKind returns the kind of extra a file is ("" for main content).
func extraKind(parts []string, base string) string {
	for _, p := range parts[:len(parts)-1] {
		if k := extrasDirs[strings.ToLower(p)]; k != "" {
			return k
		}
	}
	lower := strings.ToLower(base)
	for _, s := range extrasSuffixes {
		if strings.HasSuffix(lower, s[0]) {
			return s[1]
		}
	}
	return ""
}

var extraLabels = map[string]string{"trailer": "Trailer", "featurette": "Featurette", "behind_the_scenes": "Behind the Scenes",
	"deleted_scene": "Deleted Scene", "interview": "Interview", "scene": "Scene", "short": "Short", "other": "Extra"}

// extraTitle makes a readable title from an extra's file name.
func extraTitle(base string) string {
	lower := strings.ToLower(base)
	for _, s := range extrasSuffixes {
		if strings.HasSuffix(lower, s[0]) {
			base = base[:len(base)-len(s[0])]
			break
		}
	}
	t := strings.TrimSpace(strings.NewReplacer(".", " ", "_", " ").Replace(base))
	if t == "" {
		return "Extra"
	}
	return t
}

// placeExtra indexes a trailer, featurette or other extra. Extras are hidden from library
// lists; linkExtras attaches them to their movie or show after the scan.
func (w *writer) placeExtra(ctx context.Context, c candidate, kind, base string, parts []string) (placement, error) {
	if kind == "skip" {
		return placement{}, errSkip
	}
	title := extraTitle(base)
	// "Movie (2010)-trailer.mkv" just repeats the folder: call it what it is instead.
	if len(parts) >= 2 && strings.EqualFold(title, strings.TrimSpace(parts[0])) {
		title = extraLabels[kind]
	}
	id, created, err := w.item(ctx, "video", "x:"+c.Rel, 0, 0, itemFields{Title: title, MatchState: "local"})
	if err != nil {
		return placement{}, err
	}
	if created {
		if _, err := w.tx.ExecContext(ctx, `UPDATE items SET extra_type = ? WHERE id = ?`, kind, id); err != nil {
			return placement{}, err
		}
		w.artwork(ctx, id, "thumb", "frame", c.Path)
	}
	return placement{ItemID: id}, nil
}

func (w *writer) place(ctx context.Context, c candidate, res *probe.Result) (placement, error) {
	parts := strings.Split(c.Rel, "/")
	file := parts[len(parts)-1]
	base := strings.TrimSuffix(file, filepath.Ext(file))
	switch w.lib.Type {
	case library.Movies:
		return w.placeMovie(ctx, c, parts, file, base)
	case library.Shows, library.Anime:
		return w.placeEpisode(ctx, c, parts, file, base)
	case library.Music:
		return w.placeTrack(ctx, c, res)
	case library.Videos:
		return w.placeVideo(ctx, c, base, res)
	}
	return placement{}, errSkip
}

func (w *writer) placeMovie(ctx context.Context, c candidate, parts []string, file, base string) (placement, error) {
	if kind := extraKind(parts, base); kind != "" {
		return w.placeExtra(ctx, c, kind, base, parts)
	}
	folder := ""
	if len(parts) >= 2 {
		folder = parts[0]
	}
	m := naming.ParseMovie(folder, file)
	if m.Title == "" {
		return placement{}, errSkip
	}
	key := idKey(m.IDs, "t:"+naming.Normalize(m.Title)+fmt.Sprintf(":%d", m.Year))
	id, created, err := w.item(ctx, "movie", key, 0, 0, itemFields{Title: m.Title, Year: m.Year, MatchState: "unmatched"})
	if err != nil {
		return placement{}, err
	}
	if created {
		if err := w.externalIDs(ctx, id, m.IDs); err != nil {
			return placement{}, err
		}
		// Local artwork only when the movie has its own folder.
		if folder != "" {
			dir := filepath.Join(c.Root, folder)
			files := w.dirs[dir]
			w.localArt(ctx, id, "poster", findArtwork(dir, files, "poster", "folder", "cover", "movie", base+"-poster"))
			w.localArt(ctx, id, "backdrop", findArtwork(dir, files, "fanart", "backdrop", "background", base+"-fanart"))
		}
	}
	return placement{ItemID: id, Label: m.Edition, Part: naming.PartIndex(file)}, nil
}

func (w *writer) placeEpisode(ctx context.Context, c candidate, parts []string, file, base string) (placement, error) {
	seasonFolder := ""
	if len(parts) >= 3 {
		seasonFolder = parts[1]
	}
	if _, isSeason := naming.ParseSeasonFolder(seasonFolder); !isSeason {
		if kind := extraKind(parts, base); kind != "" {
			return w.placeExtra(ctx, c, kind, base, parts)
		}
	}
	ep, ok := naming.ParseEpisode(file)
	if !ok {
		return placement{}, errSkip
	}

	var show naming.Show
	if len(parts) >= 2 {
		show = naming.ParseShowFolder(parts[0])
	} else {
		show = naming.ShowTitleFromFile(file)
	}
	if show.Title == "" {
		return placement{}, errSkip
	}

	season := -1
	if n, ok := naming.ParseSeasonFolder(seasonFolder); ok {
		season = n
	}
	if season < 0 {
		season = ep.Season
	}
	episode := 0
	if len(ep.Episodes) > 0 {
		episode = ep.Episodes[0]
	}
	if ep.AirDate != "" {
		// Date-based shows: season = year, episode = MMDD.
		t, err := time.Parse("2006-01-02", ep.AirDate)
		if err == nil {
			if season < 0 {
				season = t.Year()
			}
			episode = int(t.Month())*100 + t.Day()
		}
	}
	if season < 0 {
		if _, sn, ok := naming.FansubShow(file); ok {
			season = sn
		}
	}
	if season < 0 {
		season = 1
	}

	showKey := idKey(show.IDs, "t:"+naming.Normalize(show.Title)+fmt.Sprintf(":%d", show.Year))
	showID, created, err := w.item(ctx, "show", showKey, 0, 0, itemFields{Title: show.Title, Year: show.Year, MatchState: "unmatched"})
	if err != nil {
		return placement{}, err
	}
	showDir := filepath.Join(c.Root, parts[0])
	if created {
		if err := w.externalIDs(ctx, showID, show.IDs); err != nil {
			return placement{}, err
		}
		if len(parts) >= 2 {
			files := w.dirs[showDir]
			w.localArt(ctx, showID, "poster", findArtwork(showDir, files, "poster", "folder", "cover", "show"))
			w.localArt(ctx, showID, "backdrop", findArtwork(showDir, files, "fanart", "backdrop", "background"))
			w.localArt(ctx, showID, "banner", findArtwork(showDir, files, "banner"))
			w.localArt(ctx, showID, "logo", findArtwork(showDir, files, "logo", "clearlogo"))
		}
	}

	seasonTitle := fmt.Sprintf("Season %d", season)
	if season == 0 {
		seasonTitle = "Specials"
	}
	seasonID, created, err := w.item(ctx, "season", fmt.Sprintf("%d:s%d", showID, season), showID, 0,
		itemFields{Idx: season, Title: seasonTitle, MatchState: "unmatched"})
	if err != nil {
		return placement{}, err
	}
	if created && len(parts) >= 2 {
		art := findArtwork(showDir, w.dirs[showDir], fmt.Sprintf("season%02d-poster", season), fmt.Sprintf("season%d-poster", season))
		if season == 0 && art == "" {
			art = findArtwork(showDir, w.dirs[showDir], "season-specials-poster")
		}
		if art == "" && seasonFolder != "" {
			sd := filepath.Join(showDir, seasonFolder)
			art = findArtwork(sd, w.dirs[sd], "poster", "folder", "cover")
		}
		w.localArt(ctx, seasonID, "poster", art)
	}

	title := ep.Title
	if title == "" {
		title = fmt.Sprintf("Episode %d", episode)
	}
	epID, created, err := w.item(ctx, "episode", fmt.Sprintf("%d:s%de%d", showID, season, episode), seasonID, showID,
		itemFields{Idx: episode, AbsIdx: ep.Absolute, Title: title, Date: ep.AirDate, MatchState: "unmatched"})
	if err != nil {
		return placement{}, err
	}
	if created {
		dir := filepath.Dir(c.Path)
		w.localArt(ctx, epID, "thumb", findArtwork(dir, w.dirs[dir], base+"-thumb", base))
	}
	return placement{ItemID: epID, Part: naming.PartIndex(file)}, nil
}

func (w *writer) placeTrack(ctx context.Context, c candidate, res *probe.Result) (placement, error) {
	t := readTrackTags(res.Tags, c.Rel)
	artists := splitArtists(t.AlbumArtist)
	primary := artists[0]

	artistID, created, err := w.item(ctx, "artist", "a:"+naming.Normalize(primary), 0, 0,
		itemFields{Title: primary, MatchState: "local"})
	if err != nil {
		return placement{}, err
	}
	if created && t.MBArtistID != "" && len(artists) == 1 {
		w.externalID(ctx, artistID, "musicbrainz", t.MBArtistID)
	}

	albumKey := "al:" + naming.Normalize(primary) + ":" + naming.Normalize(t.Album)
	albumID, created, err := w.item(ctx, "album", albumKey, artistID, 0,
		itemFields{Title: t.Album, Year: t.Year, ArtistCredit: t.AlbumArtist, MatchState: "local"})
	if err != nil {
		return placement{}, err
	}
	if created {
		if t.MBAlbumID != "" {
			w.externalID(ctx, albumID, "musicbrainz", t.MBAlbumID)
		}
		dir := filepath.Dir(c.Path)
		art := findArtwork(dir, w.dirs[dir], "cover", "folder", "front", "album", "albumart")
		if art == "" {
			for _, s := range res.Streams {
				if s.AttachedPic {
					// Embedded cover: extracted from the audio file by the image service.
					w.artwork(ctx, albumID, "poster", "embedded", c.Path)
					break
				}
			}
		} else {
			w.localArt(ctx, albumID, "poster", art)
		}
		if t.Genre != "" {
			for _, g := range strings.FieldsFunc(t.Genre, func(r rune) bool { return r == ';' || r == '/' || r == ',' }) {
				w.tag(ctx, albumID, "genre", strings.TrimSpace(g))
			}
		}
	}

	trackID, _, err := w.item(ctx, "track", "f:"+c.Rel, albumID, artistID,
		itemFields{Idx: t.Track, Disc: t.Disc, Title: t.Title, ArtistCredit: t.Artist, MatchState: "local"})
	if err != nil {
		return placement{}, err
	}
	if t.MBTrackID != "" {
		w.externalID(ctx, trackID, "musicbrainz", t.MBTrackID)
	}
	return placement{ItemID: trackID}, nil
}

func (w *writer) placeVideo(ctx context.Context, c candidate, base string, res *probe.Result) (placement, error) {
	m := naming.ParseMovie("", base+".x")
	title := m.Title
	if title == "" {
		title = base
	}
	f := itemFields{Title: title, Year: m.Year, MatchState: "local"}
	if ct := res.Tags["creation_time"]; len(ct) >= 10 {
		if t, err := time.Parse("2006-01-02", ct[:10]); err == nil && t.Year() > 1970 {
			f.Date = ct[:10]
			if f.Year == 0 {
				f.Year = t.Year()
			}
		}
	}
	id, created, err := w.item(ctx, "video", "f:"+c.Rel, 0, 0, f)
	if err != nil {
		return placement{}, err
	}
	if created {
		// Personal videos have no online artwork: use a frame from the video (META-4).
		dir := filepath.Dir(c.Path)
		if art := findArtwork(dir, w.dirs[dir], base+"-thumb", base); art != "" {
			w.localArt(ctx, id, "thumb", art)
		} else {
			w.artwork(ctx, id, "thumb", "frame", c.Path)
		}
	}
	return placement{ItemID: id}, nil
}

// item finds an item by scan key or creates it. Existing items are never overwritten here:
// their titles may have been matched or edited.
func (w *writer) item(ctx context.Context, typ, key string, parentID, grandparentID int64, f itemFields) (int64, bool, error) {
	ck := typ + "|" + key
	if id, ok := w.keyCache[ck]; ok {
		return id, false, nil
	}
	var id int64
	err := w.tx.QueryRowContext(ctx, `SELECT id FROM items WHERE library_id = ? AND type = ? AND scan_key = ?`, w.lib.ID, typ, key).Scan(&id)
	if err == nil {
		w.keyCache[ck] = id
		return id, false, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, err
	}
	sortTitle := naming.SortTitle(f.Title)
	if typ == "season" || typ == "episode" || typ == "track" {
		sortTitle = fmt.Sprintf("%05d %05d", f.Disc, f.Idx)
	}
	r, err := w.tx.ExecContext(ctx, `INSERT INTO items
		(library_id, type, scan_key, parent_id, grandparent_id, idx, absolute_idx, disc, title, sort_title,
		 year, originally_available_at, artist_credit, match_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.lib.ID, typ, key, nullInt64(parentID), nullInt64(grandparentID), nullIdx(typ, f.Idx), nullInt(f.AbsIdx), nullInt(f.Disc),
		f.Title, sortTitle, nullInt(f.Year), nullStr(f.Date), nullStr(f.ArtistCredit), f.MatchState)
	if err != nil {
		return 0, false, err
	}
	id, _ = r.LastInsertId()
	w.keyCache[ck] = id
	return id, true, nil
}

func (w *writer) externalIDs(ctx context.Context, itemID int64, ids naming.IDs) error {
	for provider, v := range map[string]string{"tmdb": ids.TMDB, "tvdb": ids.TVDB, "imdb": ids.IMDB, "anidb": ids.AniDB} {
		if v != "" {
			if err := w.externalID(ctx, itemID, provider, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *writer) externalID(ctx context.Context, itemID int64, provider, value string) error {
	_, err := w.tx.ExecContext(ctx, `INSERT OR IGNORE INTO external_ids(item_id, provider, value) VALUES (?, ?, ?)`, itemID, provider, value)
	return err
}

func (w *writer) localArt(ctx context.Context, itemID int64, kind, path string) {
	if path != "" {
		w.artwork(ctx, itemID, kind, "local", path)
	}
}

// artwork records a local or embedded image. Local art is selected by default; online
// agents only replace the selection when no local art exists.
func (w *writer) artwork(ctx context.Context, itemID int64, kind, source, path string) {
	w.tx.ExecContext(ctx, `INSERT INTO artwork(item_id, kind, source, local_path, selected)
		SELECT ?, ?, ?, ?, NOT EXISTS (SELECT 1 FROM artwork WHERE item_id = ? AND kind = ? AND selected = 1)`,
		itemID, kind, source, path, itemID, kind)
}

func (w *writer) tag(ctx context.Context, itemID int64, kind, name string) {
	if name == "" {
		return
	}
	w.tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(kind, name) VALUES (?, ?)`, kind, name)
	w.tx.ExecContext(ctx, `INSERT OR IGNORE INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind = ? AND name = ?`, itemID, kind, name)
}

func idKey(ids naming.IDs, fallback string) string {
	switch {
	case ids.TMDB != "":
		return "tmdb:" + ids.TMDB
	case ids.TVDB != "":
		return "tvdb:" + ids.TVDB
	case ids.IMDB != "":
		return "imdb:" + ids.IMDB
	}
	return fallback
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// nullIdx keeps index 0 for seasons/episodes/tracks (Specials is season 0).
func nullIdx(typ string, n int) any {
	if typ == "season" || typ == "episode" || typ == "track" {
		return n
	}
	return nullInt(n)
}
