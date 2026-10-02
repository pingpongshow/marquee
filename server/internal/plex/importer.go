package plex

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/auth"
	"marquee/internal/library"
	"marquee/internal/scanner/naming"
)

// Matcher applies a TMDB match to an item (metadata.Service.MatchTo).
type Matcher interface {
	MatchTo(ctx context.Context, itemID int64, tmdbID int) error
}

type Importer struct {
	DB        *sql.DB
	Auth      *auth.Service
	Libraries *library.Store
	Matcher   Matcher
	PlexRoot  string // folder containing Plex's "Library" folder (mounted at /plex)
	WorkDir   string // scratch space for the database snapshot
	ArtDir    string // where imported posters are kept

	mu       sync.Mutex
	running  bool
	progress Progress
	lastErr  string
}

type Progress struct {
	Step        string
	Done, Total int
}

// PathMapping rewrites a Plex path prefix to a Marquee path prefix.
type PathMapping struct{ Plex, Marquee string }

type SectionPreview struct {
	Section
	LibraryID           int64 // matching Marquee library, 0 if none
	Files, MatchedFiles int
}

type AccountPreview struct {
	Account
	IsOwner           bool
	SuggestedAction   string // link, create
	SuggestedUserID   int64
	SuggestedUsername string
}

type Preview struct {
	Sections        []SectionPreview
	Accounts        []AccountPreview
	Mappings        []PathMapping
	Files, Matched  int
	UnmatchedSample []string
}

// AccountChoice says what to do with a Plex account.
type AccountChoice struct {
	PlexID    int64
	Action    string // create, link, merge, skip
	UserID    int64  // for link
	Username  string // for create
	MergeInto int64  // for merge: the Plex account whose Marquee user this one joins
}

type Options struct {
	Mappings                                                  []PathMapping
	Accounts                                                  []AccountChoice
	Matches, WatchState, History, Playlists, Markers, Artwork bool
}

type Report struct {
	StartedAt, FinishedAt                     time.Time
	SnapshotAt                                time.Time
	Files, MatchedFiles                       int
	UnmatchedSample                           []string
	UsersCreated, UsersLinked                 int
	MatchesAgreed, MatchesApplied             int
	MatchesKept                               []string // disagreements where Marquee's match was kept
	MatchFailures                             []string
	Posters, PostersMissing                   int
	WatchStateItems, Ratings                  int
	HistoryImported, HistorySkipped           int
	Markers                                   int
	Playlists, PlaylistItems, PlaylistMissing int
	Warnings                                  []string
}

const reportKey = "plex_import_report"

// Available reports whether a Plex database can be found.
func (im *Importer) Available() (string, bool) {
	p, err := DatabasePath(im.PlexRoot)
	return p, err == nil
}

// Status returns whether an import is running, its progress and the last error.
func (im *Importer) Status() (bool, Progress, string) {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.running, im.progress, im.lastErr
}

func (im *Importer) setProgress(step string, done, total int) {
	im.mu.Lock()
	im.progress = Progress{Step: step, Done: done, Total: total}
	im.mu.Unlock()
}

// LastReport returns the report of the most recent completed import.
func (im *Importer) LastReport(ctx context.Context) (*Report, error) {
	var raw string
	err := im.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, reportKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Report
	return &r, json.Unmarshal([]byte(raw), &r)
}

// marqueeFiles maps every Marquee media file path to its item.
func (im *Importer) marqueeFiles(ctx context.Context) (map[string]int64, error) {
	rows, err := im.DB.QueryContext(ctx, `SELECT f.path, v.item_id FROM media_files f JOIN media_versions v ON v.id = f.version_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var p string
		var id int64
		if err := rows.Scan(&p, &id); err != nil {
			return nil, err
		}
		out[p] = id
	}
	return out, rows.Err()
}

func remap(p string, mappings []PathMapping) (string, bool) {
	best := -1
	for i, m := range mappings {
		from := strings.TrimSuffix(m.Plex, "/")
		if (p == from || strings.HasPrefix(p, from+"/")) && (best < 0 || len(from) > len(strings.TrimSuffix(mappings[best].Plex, "/"))) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	m := mappings[best]
	return strings.TrimSuffix(m.Marquee, "/") + strings.TrimPrefix(p, strings.TrimSuffix(m.Plex, "/")), true
}

var sectionTypes = map[int][]library.Type{1: {library.Movies}, 2: {library.Shows, library.Anime}, 8: {library.Music}}

// defaultMappings pairs each Plex section with the Marquee library of a compatible type,
// preferring the same name, and maps its folders.
func defaultMappings(sections []Section, libs []library.Library) ([]PathMapping, map[int64]int64) {
	var maps []PathMapping
	secLib := map[int64]int64{}
	for _, sec := range sections {
		var pick *library.Library
		for i := range libs {
			l := &libs[i]
			compatible := false
			for _, t := range sectionTypes[sec.Type] {
				compatible = compatible || l.Type == t
			}
			if !compatible {
				continue
			}
			if strings.EqualFold(l.Name, sec.Name) {
				pick = l
				break
			}
			if pick == nil {
				pick = l
			}
		}
		if pick == nil {
			continue
		}
		secLib[sec.ID] = pick.ID
		for i, root := range sec.Roots {
			target := pick.Paths[min(i, len(pick.Paths)-1)]
			// Prefer a Marquee folder with the same final name ("/movies" ↔ ".../Movies").
			for _, p := range pick.Paths {
				if strings.EqualFold(filepath.Base(p), filepath.Base(root)) {
					target = p
				}
			}
			maps = append(maps, PathMapping{Plex: root, Marquee: target})
		}
	}
	return maps, secLib
}

// Preview summarises what an import would do, with suggested mappings and account actions.
func (im *Importer) Preview(ctx context.Context, mappings []PathMapping) (*Preview, error) {
	src, err := Snapshot(ctx, im.PlexRoot, im.WorkDir)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	sections, err := src.Sections(ctx)
	if err != nil {
		return nil, err
	}
	libs, err := im.Libraries.List(ctx)
	if err != nil {
		return nil, err
	}
	defMaps, secLib := defaultMappings(sections, libs)
	if len(mappings) == 0 {
		mappings = defMaps
	}
	files, err := im.marqueeFiles(ctx)
	if err != nil {
		return nil, err
	}
	parts, err := src.Parts(ctx)
	if err != nil {
		return nil, err
	}
	pv := &Preview{Mappings: mappings, Sections: make([]SectionPreview, len(sections))}
	perSec := map[int64]*SectionPreview{}
	for i, s := range sections {
		pv.Sections[i] = SectionPreview{Section: s, LibraryID: secLib[s.ID]}
		perSec[s.ID] = &pv.Sections[i]
	}
	for _, p := range parts {
		pv.Files++
		sp := perSec[p.SectionID]
		if sp != nil {
			sp.Files++
		}
		if mp, ok := remap(p.File, mappings); ok {
			if _, found := files[mp]; found {
				pv.Matched++
				if sp != nil {
					sp.MatchedFiles++
				}
				continue
			}
		}
		if len(pv.UnmatchedSample) < 25 {
			pv.UnmatchedSample = append(pv.UnmatchedSample, p.File)
		}
	}

	accounts, err := src.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	users, err := im.Auth.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	var ownerID int64
	for _, u := range users {
		if u.IsAdmin {
			ownerID = u.ID
			break
		}
	}
	for _, a := range accounts {
		ap := AccountPreview{Account: a, IsOwner: a.ID == 1, SuggestedAction: "create", SuggestedUsername: sanitizeUsername(a.Name)}
		if a.ID == 1 && ownerID != 0 {
			ap.SuggestedAction, ap.SuggestedUserID = "link", ownerID
		}
		for _, u := range users {
			if strings.EqualFold(u.Username, ap.SuggestedUsername) || strings.EqualFold(u.DisplayName, a.Name) {
				ap.SuggestedAction, ap.SuggestedUserID = "link", u.ID
			}
		}
		pv.Accounts = append(pv.Accounts, ap)
	}
	return pv, nil
}

func sanitizeUsername(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// Start runs an import in the background. It returns an error if one is already running.
func (im *Importer) Start(ctx context.Context, opts Options) error {
	im.mu.Lock()
	if im.running {
		im.mu.Unlock()
		return errors.New("an import is already running")
	}
	im.running, im.lastErr = true, ""
	im.progress = Progress{Step: "Copying the Plex database"}
	im.mu.Unlock()
	go func() {
		rep, err := im.run(ctx, opts)
		im.mu.Lock()
		im.running = false
		if err != nil {
			im.lastErr = err.Error()
			slog.Error("Plex import failed", "err", err)
		}
		im.mu.Unlock()
		if rep != nil {
			raw, _ := json.Marshal(rep)
			im.DB.ExecContext(context.Background(), `INSERT INTO settings(key, value) VALUES (?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, reportKey, string(raw))
			slog.Info("Plex import finished", "files", rep.MatchedFiles, "watchState", rep.WatchStateItems,
				"history", rep.HistoryImported, "playlists", rep.Playlists, "matchesApplied", rep.MatchesApplied)
		}
	}()
	return nil
}

func iso(unix int64) string { return time.Unix(unix, 0).UTC().Format("2006-01-02T15:04:05.000Z") }

func (im *Importer) run(ctx context.Context, opts Options) (*Report, error) {
	rep := &Report{StartedAt: time.Now().UTC()}
	src, err := Snapshot(ctx, im.PlexRoot, im.WorkDir)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	rep.SnapshotAt = src.TakenAt

	// ---- items: Plex leaf → Marquee item, then containers through the hierarchy ----
	im.setProgress("Pairing Plex items with Marquee files", 0, 0)
	files, err := im.marqueeFiles(ctx)
	if err != nil {
		return rep, err
	}
	parts, err := src.Parts(ctx)
	if err != nil {
		return rep, err
	}
	plexItems, err := src.Items(ctx)
	if err != nil {
		return rep, err
	}
	toOurs := map[int64]int64{}    // Plex item id → Marquee item id
	leafPath := map[int64]string{} // Plex leaf id → file path (Marquee side)
	for _, p := range parts {
		rep.Files++
		if mp, ok := remap(p.File, opts.Mappings); ok {
			if id, found := files[mp]; found {
				toOurs[p.ItemID] = id
				leafPath[p.ItemID] = mp
				rep.MatchedFiles++
				continue
			}
		}
		if len(rep.UnmatchedSample) < 25 {
			rep.UnmatchedSample = append(rep.UnmatchedSample, p.File)
		}
	}
	if err := im.mapContainers(ctx, plexItems, toOurs); err != nil {
		return rep, err
	}
	byGUID := map[string][]int64{} // Plex guid → Marquee items
	for pid, ours := range toOurs {
		if g := plexItems[pid].GUID; g != "" {
			byGUID[g] = append(byGUID[g], ours)
		}
	}

	// ---- users ----
	im.setProgress("Setting up users", 0, len(opts.Accounts))
	userFor := map[int64]int64{} // Plex account → Marquee user
	for _, a := range opts.Accounts {
		switch a.Action {
		case "link":
			if _, err := im.Auth.GetUser(ctx, a.UserID); err != nil {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %d: Marquee user %d not found", a.PlexID, a.UserID))
				continue
			}
			userFor[a.PlexID] = a.UserID
			rep.UsersLinked++
		case "create":
			u, err := im.createUser(ctx, a.Username)
			if err != nil {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %q: %v", a.Username, err))
				continue
			}
			userFor[a.PlexID] = u
			rep.UsersCreated++
		}
	}
	// Merges resolve after creates/links so they can point at a user created in this run.
	for _, a := range opts.Accounts {
		if a.Action != "merge" {
			continue
		}
		if u, ok := userFor[a.MergeInto]; ok && a.MergeInto != a.PlexID {
			userFor[a.PlexID] = u
			rep.UsersLinked++
		} else {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %d: merge target %d isn't being imported", a.PlexID, a.MergeInto))
		}
	}

	if opts.Matches {
		if err := im.importMatches(ctx, src, plexItems, toOurs, leafPath, opts.Mappings, rep); err != nil {
			return rep, err
		}
	}
	if opts.Artwork {
		im.importArtwork(ctx, src, plexItems, toOurs, rep)
	}
	if opts.WatchState {
		if err := im.importWatchState(ctx, src, byGUID, userFor, rep); err != nil {
			return rep, err
		}
	}
	if opts.History {
		if err := im.importHistory(ctx, src, byGUID, userFor, rep); err != nil {
			return rep, err
		}
	}
	if opts.Markers {
		if err := im.importMarkers(ctx, src, toOurs, rep); err != nil {
			return rep, err
		}
	}
	if opts.Playlists {
		if err := im.importPlaylists(ctx, src, plexItems, toOurs, userFor, rep); err != nil {
			return rep, err
		}
	}
	rep.FinishedAt = time.Now().UTC()
	return rep, nil
}

// mapContainers maps Plex seasons/shows/albums/artists to Marquee's through the leaf items:
// a Plex season maps to the Marquee season holding most of its episodes. The vote keeps
// re-runs stable when the two servers group episodes differently.
func (im *Importer) mapContainers(ctx context.Context, plexItems map[int64]Item, toOurs map[int64]int64) error {
	parent := map[int64]int64{}
	rows, err := im.DB.QueryContext(ctx, `SELECT id, COALESCE(parent_id, 0) FROM items`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, p int64
		rows.Scan(&id, &p)
		parent[id] = p
	}
	rows.Close()
	level := map[int64]int64{}
	for k, v := range toOurs {
		level[k] = v
	}
	for depth := 0; depth < 2; depth++ {
		votes := map[int64]map[int64]int{}
		for plexID, ours := range level {
			pp, op := plexItems[plexID].ParentID, parent[ours]
			if pp == 0 || op == 0 {
				continue
			}
			if votes[pp] == nil {
				votes[pp] = map[int64]int{}
			}
			votes[pp][op]++
		}
		next := map[int64]int64{}
		for pp, counts := range votes {
			var best, bestN int64 = 0, -1
			for op, n := range counts {
				if int64(n) > bestN || (int64(n) == bestN && op < best) {
					best, bestN = op, int64(n)
				}
			}
			next[pp] = best
			if _, mapped := toOurs[pp]; !mapped {
				toOurs[pp] = best
			}
		}
		level = next
	}
	return nil
}

func (im *Importer) createUser(ctx context.Context, username string) (int64, error) {
	username = sanitizeUsername(username)
	if username == "" {
		return 0, errors.New("empty username")
	}
	name := username
	for i := 2; ; i++ {
		u, err := im.Auth.Create(ctx, auth.NewUser{Username: name, DisplayName: username, Imported: true})
		if errors.Is(err, auth.ErrUsernameTaken) && i < 20 {
			// Re-running the import links to the user created last time.
			users, _ := im.Auth.ListUsers(ctx)
			for _, x := range users {
				if strings.EqualFold(x.Username, name) {
					return x.ID, nil
				}
			}
			name = fmt.Sprintf("%s%d", username, i)
			continue
		}
		if err != nil {
			return 0, err
		}
		return u.ID, nil
	}
}

// importMatches reconciles movie and show matches. Plex wins only where it is likely right:
// the owner edited the item in Plex, or Marquee has no match and Plex's title fits the file
// name, or Plex's title fits the file name better than Marquee's. Remaining disagreements are
// listed in the report for review with Fix Match.
func (im *Importer) importMatches(ctx context.Context, src *Source, plexItems map[int64]Item, toOurs map[int64]int64,
	leafPath map[int64]string, mappings []PathMapping, rep *Report) error {
	ext, err := src.ExternalIDs(ctx)
	if err != nil {
		return err
	}
	// The name each movie/show was scanned from: movie folder or file, show folder.
	parsed := map[int64]naming.Movie{}
	for leaf, path := range leafPath {
		it := plexItems[leaf]
		rel := relToMapping(path, mappings)
		parts := strings.Split(rel, "/")
		switch it.Type {
		case typeMovie:
			folder := ""
			if len(parts) >= 2 {
				folder = parts[0]
			}
			parsed[leaf] = naming.ParseMovie(folder, parts[len(parts)-1])
		case typeEpisode:
			show := plexItems[plexItems[it.ParentID].ParentID]
			if _, done := parsed[show.ID]; !done && len(parts) >= 2 {
				sh := naming.ParseShowFolder(parts[0])
				parsed[show.ID] = naming.Movie{Title: sh.Title, Year: sh.Year}
			}
		}
	}

	type job struct {
		ours  int64
		tmdb  int
		title string
	}
	var jobs []job
	for pid, ours := range toOurs {
		it := plexItems[pid]
		if it.Type != typeMovie && it.Type != typeShow {
			continue
		}
		plexTMDB, _ := strconv.Atoi(ext[pid]["tmdb"])
		if plexTMDB == 0 {
			continue // Plex never matched it
		}
		var ourTMDB sql.NullString
		var ourTitle, state string
		var ourYear int
		im.DB.QueryRowContext(ctx, `SELECT i.title, COALESCE(i.year, 0), i.match_state,
			(SELECT value FROM external_ids WHERE item_id = i.id AND provider = 'tmdb') FROM items i WHERE i.id = ?`, ours).
			Scan(&ourTitle, &ourYear, &state, &ourTMDB)
		if ourTMDB.String == strconv.Itoa(plexTMDB) {
			rep.MatchesAgreed++
			continue
		}
		name := parsed[pid]
		plexScore := naming.MatchScore(it.Title, it.Year, name.Title, name.Year)
		ourScore := naming.MatchScore(ourTitle, ourYear, name.Title, name.Year)
		edited := len(lockedFields(it.UserFields)) > 0
		switch {
		case edited, state != "matched" && plexScore >= 3, state == "matched" && plexScore > ourScore:
			jobs = append(jobs, job{ours, plexTMDB, it.Title})
		case state == "matched":
			rep.MatchesKept = append(rep.MatchesKept, fmt.Sprintf("%q: kept %q (%d); Plex had %q (%d)", name.Title, ourTitle, ourYear, it.Title, it.Year))
		default:
			rep.MatchFailures = append(rep.MatchFailures, fmt.Sprintf("%q: Plex's %q (%d) doesn't fit the file name; left unmatched", name.Title, it.Title, it.Year))
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ours < jobs[j].ours })
	sort.Strings(rep.MatchesKept)
	for i, j := range jobs {
		im.setProgress("Applying Plex matches", i, len(jobs))
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := im.Matcher.MatchTo(ctx, j.ours, j.tmdb); err != nil {
			rep.MatchFailures = append(rep.MatchFailures, fmt.Sprintf("%s (tmdb %d): %v", j.title, j.tmdb, err))
			continue
		}
		rep.MatchesApplied++
	}
	return nil
}

// relToMapping returns path relative to the Marquee folder of the mapping that contains it.
func relToMapping(path string, mappings []PathMapping) string {
	best := ""
	for _, m := range mappings {
		root := strings.TrimSuffix(m.Marquee, "/")
		if strings.HasPrefix(path, root+"/") && len(root) > len(best) {
			best = root
		}
	}
	if best == "" {
		return filepath.Base(path)
	}
	return strings.TrimPrefix(path, best+"/")
}

// importArtwork copies posters/backgrounds the owner chose in Plex (locked fields 9 and 10).
func (im *Importer) importArtwork(ctx context.Context, src *Source, plexItems map[int64]Item, toOurs map[int64]int64, rep *Report) {
	im.setProgress("Copying chosen artwork", 0, 0)
	for pid, ours := range toOurs {
		it := plexItems[pid]
		locks := lockedFields(it.UserFields)
		for _, a := range []struct {
			lock int
			kind string
			ref  string
		}{{9, "poster", it.UserThumb}, {10, "backdrop", it.UserArt}} {
			if !locks[a.lock] || a.ref == "" {
				continue
			}
			file := src.ArtworkFile(it, a.ref)
			if file == "" {
				rep.PostersMissing++
				continue
			}
			dst, err := im.keepArt(file)
			if err != nil {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("artwork for %s: %v", it.Title, err))
				continue
			}
			tx, err := im.DB.BeginTx(ctx, nil)
			if err != nil {
				continue
			}
			tx.ExecContext(ctx, `DELETE FROM artwork WHERE item_id = ? AND kind = ? AND source = 'plex'`, ours, a.kind)
			tx.ExecContext(ctx, `UPDATE artwork SET selected = 0 WHERE item_id = ? AND kind = ?`, ours, a.kind)
			tx.ExecContext(ctx, `INSERT INTO artwork(item_id, kind, source, local_path, selected) VALUES (?, ?, 'plex', ?, 1)`, ours, a.kind, dst)
			if tx.Commit() == nil {
				rep.Posters++
			}
		}
	}
}

// keepArt copies an image into Marquee's config so it survives removing the Plex mount.
func (im *Importer) keepArt(file string) (string, error) {
	h := sha1.Sum([]byte(file))
	dst := filepath.Join(im.ArtDir, hex.EncodeToString(h[:])+".img")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	if err := os.MkdirAll(im.ArtDir, 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	return dst, out.Close()
}

// lockedFields parses Plex user_fields ("lockedFields=9|10").
func lockedFields(userFields string) map[int]bool {
	out := map[int]bool{}
	for _, kv := range strings.Split(userFields, "&") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k != "lockedFields" {
			continue
		}
		for _, f := range strings.Split(v, "|") {
			if n, err := strconv.Atoi(f); err == nil {
				out[n] = true
			}
		}
	}
	return out
}

// importWatchState merges Plex watch state into Marquee's: play counts take the maximum,
// and the resume position comes from whichever side was watched most recently.
func (im *Importer) importWatchState(ctx context.Context, src *Source, byGUID map[string][]int64, userFor map[int64]int64, rep *Report) error {
	settings, err := src.Settings(ctx)
	if err != nil {
		return err
	}
	tx, err := im.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, st := range settings {
		if i%500 == 0 {
			im.setProgress("Importing watch status", i, len(settings))
		}
		user, ok := userFor[st.AccountID]
		if !ok {
			continue
		}
		items := byGUID[st.GUID]
		if len(items) == 0 || (st.ViewCount == 0 && st.ViewOffsetMS == 0 && st.Rating == 0) {
			continue
		}
		var last any
		if st.LastViewedAt > 0 {
			last = iso(st.LastViewedAt)
		}
		var rating any
		if st.Rating > 0 {
			rating = st.Rating
			rep.Ratings++
		}
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_item_state(user_id, item_id, view_offset_ms, play_count, last_viewed_at, rating)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(user_id, item_id) DO UPDATE SET
					play_count = MAX(play_count, excluded.play_count),
					view_offset_ms = CASE WHEN excluded.last_viewed_at IS NOT NULL
						AND (last_viewed_at IS NULL OR excluded.last_viewed_at > last_viewed_at)
						THEN excluded.view_offset_ms ELSE view_offset_ms END,
					last_viewed_at = MAX(COALESCE(last_viewed_at, ''), COALESCE(excluded.last_viewed_at, '')),
					rating = COALESCE(rating, excluded.rating),
					updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
				user, item, st.ViewOffsetMS, st.ViewCount, last, rating); err != nil {
				return err
			}
			rep.WatchStateItems++
		}
	}
	// MAX('') leaves an empty string where neither side had a date.
	if _, err := tx.ExecContext(ctx, `UPDATE user_item_state SET last_viewed_at = NULL WHERE last_viewed_at = ''`); err != nil {
		return err
	}
	return tx.Commit()
}

func (im *Importer) importHistory(ctx context.Context, src *Source, byGUID map[string][]int64, userFor map[int64]int64, rep *Report) error {
	views, err := src.Views(ctx)
	if err != nil {
		return err
	}
	tx, err := im.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, v := range views {
		if i%500 == 0 {
			im.setProgress("Importing play history", i, len(views))
		}
		user, ok := userFor[v.AccountID]
		if !ok {
			continue
		}
		title := v.Title
		if v.GrandparentTitle != "" {
			title = v.GrandparentTitle + " – " + v.Title
		}
		var item any
		if ids := byGUID[v.GUID]; len(ids) > 0 {
			item = ids[0]
		}
		started := iso(v.ViewedAt)
		var exists int
		tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM play_history WHERE user_id = ? AND started_at = ? AND item_title = ? AND source = 'plex'`,
			user, started, title).Scan(&exists)
		if exists > 0 {
			rep.HistorySkipped++
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO play_history(user_id, item_id, item_title, started_at, stopped_at, source)
			VALUES (?, ?, ?, ?, ?, 'plex')`, user, item, title, started, started); err != nil {
			return err
		}
		rep.HistoryImported++
	}
	return tx.Commit()
}

func (im *Importer) importMarkers(ctx context.Context, src *Source, toOurs map[int64]int64, rep *Report) error {
	markers, err := src.Markers(ctx)
	if err != nil {
		return err
	}
	im.setProgress("Importing intro and credits markers", 0, len(markers))
	tx, err := im.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cleared := map[int64]bool{}
	for _, m := range markers {
		ours, ok := toOurs[m.ItemID]
		if !ok {
			continue
		}
		rows, err := tx.QueryContext(ctx, `SELECT f.id FROM media_files f JOIN media_versions v ON v.id = f.version_id WHERE v.item_id = ?`, ours)
		if err != nil {
			return err
		}
		var fileIDs []int64
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			fileIDs = append(fileIDs, id)
		}
		rows.Close()
		for _, f := range fileIDs {
			if !cleared[f] {
				if _, err := tx.ExecContext(ctx, `DELETE FROM markers WHERE file_id = ? AND source = 'plex'`, f); err != nil {
					return err
				}
				cleared[f] = true
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO markers(file_id, kind, start_ms, end_ms, source) VALUES (?, ?, ?, ?, 'plex')`,
				f, m.Kind, m.StartMS, m.EndMS); err != nil {
				return err
			}
		}
		rep.Markers++
	}
	return tx.Commit()
}

func (im *Importer) importPlaylists(ctx context.Context, src *Source, plexItems map[int64]Item, toOurs map[int64]int64, userFor map[int64]int64, rep *Report) error {
	lists, err := src.Playlists(ctx)
	if err != nil {
		return err
	}
	im.setProgress("Importing playlists", 0, len(lists))
	for _, pl := range lists {
		user, ok := userFor[pl.OwnerID]
		if !ok {
			continue
		}
		var ids []int64
		kind := "video"
		for _, pid := range pl.ItemIDs {
			ours, ok := toOurs[pid]
			if !ok {
				rep.PlaylistMissing++
				continue
			}
			if plexItems[pid].Type == typeTrack {
				kind = "audio"
			}
			ids = append(ids, ours)
		}
		if len(ids) == 0 {
			continue
		}
		tx, err := im.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		// Re-running replaces the playlist with the same title for the same user.
		tx.ExecContext(ctx, `DELETE FROM playlists WHERE user_id = ? AND title = ? AND smart_rule IS NULL`, user, pl.Title)
		r, err := tx.ExecContext(ctx, `INSERT INTO playlists(user_id, title, kind) VALUES (?, ?, ?)`, user, pl.Title, kind)
		if err != nil {
			tx.Rollback()
			return err
		}
		plID, _ := r.LastInsertId()
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items(playlist_id, item_id, ord) VALUES (?, ?, ?)`, plID, id, float64(i+1)); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		rep.Playlists++
		rep.PlaylistItems += len(ids)
	}
	return nil
}

// CheckArtwork counts chosen (locked) Plex artwork whose image file can be found.
func (im *Importer) CheckArtwork(ctx context.Context) (found, missing int) {
	src, err := Snapshot(ctx, im.PlexRoot, im.WorkDir)
	if err != nil {
		return 0, 0
	}
	defer src.Close()
	items, err := src.Items(ctx)
	if err != nil {
		return 0, 0
	}
	for _, it := range items {
		locks := lockedFields(it.UserFields)
		for lock, ref := range map[int]string{9: it.UserThumb, 10: it.UserArt} {
			if !locks[lock] || ref == "" {
				continue
			}
			if src.ArtworkFile(it, ref) != "" {
				found++
			} else {
				missing++
			}
		}
	}
	return found, missing
}
