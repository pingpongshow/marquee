package subtitles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ErrNotVideo is returned for items that can't have subtitles.
var ErrNotVideo = errors.New("only movies, episodes and videos have subtitles")

// Service searches for an item's subtitles and attaches downloads to it.
type Service struct {
	DB  *sql.DB
	Dir string // <config>/subtitles
	// Config returns the current OpenSubtitles settings.
	Config func() Config

	mu     sync.Mutex
	cached *Client
}

// Config is how to reach OpenSubtitles.
type Config struct{ APIKey, Username, Password, UserAgent, Base string }

// client reuses one Client (and its login token) until the settings change.
func (s *Service) client() *Client {
	cfg := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.cached; c == nil || c.APIKey != cfg.APIKey || c.Username != cfg.Username || c.Password != cfg.Password {
		s.cached = &Client{APIKey: cfg.APIKey, Username: cfg.Username, Password: cfg.Password, UserAgent: cfg.UserAgent, Base: cfg.Base}
	}
	return s.cached
}

type target struct {
	fileID   int64
	path     string
	typ      string
	title    string
	year     int
	season   int
	episode  int
	imdb     string
	tmdb     string
	language string
}

func (s *Service) target(ctx context.Context, itemID int64) (target, error) {
	var t target
	var year, season, episode sql.NullInt64
	var showID sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT f.id, f.path, i.type, COALESCE(g.title, i.title), COALESCE(i.year, g.year), p.idx, i.idx, i.grandparent_id
		FROM items i
		JOIN media_versions v ON v.item_id = i.id JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id
		WHERE i.id = ? ORDER BY v.id LIMIT 1`, itemID).Scan(&t.fileID, &t.path, &t.typ, &t.title, &year, &season, &episode, &showID)
	if err != nil {
		return t, err
	}
	if t.typ != "movie" && t.typ != "episode" && t.typ != "video" {
		return t, ErrNotVideo
	}
	t.year, t.season, t.episode = int(year.Int64), int(season.Int64), int(episode.Int64)
	idsOf := itemID
	if t.typ == "episode" && showID.Valid {
		idsOf = showID.Int64
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT provider, value FROM external_ids WHERE item_id = ? AND provider IN ('imdb', 'tmdb')`, idsOf)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var p, v string
		if rows.Scan(&p, &v) == nil {
			if p == "imdb" {
				t.imdb = v
			} else {
				t.tmdb = v
			}
		}
	}
	return t, rows.Err()
}

// Search looks for subtitles for an item in the given languages (ISO 639-1, comma-separated).
func (s *Service) Search(ctx context.Context, itemID int64, languages string) ([]Result, error) {
	t, err := s.target(ctx, itemID)
	if err != nil {
		return nil, err
	}
	q := Query{IMDbID: t.imdb, TMDBID: t.tmdb, IsEpisode: t.typ == "episode", Season: t.season, Episode: t.episode,
		Languages: languages, Title: t.title, Year: t.year}
	if h, err := Hash(t.path); err == nil {
		q.MovieHash = h
	}
	return s.client().Search(ctx, q)
}

// Download fetches a subtitle and attaches it to the item's file. It returns the new
// stream's id and how many downloads OpenSubtitles allows today.
func (s *Service) Download(ctx context.Context, itemID, providerFileID int64, language, release string, hearingImpaired bool) (int64, int, error) {
	t, err := s.target(ctx, itemID)
	if err != nil {
		return 0, 0, err
	}
	data, _, remaining, err := s.client().Download(ctx, providerFileID)
	if err != nil {
		return 0, 0, err
	}
	lang := ISO6392(language)
	dir := filepath.Join(s.Dir, strconv.FormatInt(t.fileID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, 0, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.%s.srt", providerFileID, lang))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, 0, err
	}
	title := "OpenSubtitles"
	if r := strings.TrimSpace(release); r != "" {
		title += " · " + r
	}
	if len(title) > 120 {
		title = title[:120]
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO downloaded_subtitles (file_id, path, language, title, hearing_impaired, provider_id)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(path) DO NOTHING`, t.fileID, path, lang, title, hearingImpaired, strconv.FormatInt(providerFileID, 10)); err != nil {
		return 0, 0, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM streams WHERE file_id = ? AND external_path = ?`, t.fileID, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, `INSERT INTO streams (file_id, kind, codec, language, title, is_hearing_impaired, external_path)
			VALUES (?, 'subtitle', 'subrip', ?, ?, ?, ?)`, t.fileID, lang, title, hearingImpaired, path)
		if err != nil {
			return 0, 0, err
		}
		id, _ = res.LastInsertId()
	} else if err != nil {
		return 0, 0, err
	}
	return id, remaining, tx.Commit()
}

// Remove deletes a downloaded subtitle (files from the media folder can't be removed).
func (s *Service) Remove(ctx context.Context, streamID int64) error {
	var path string
	err := s.DB.QueryRowContext(ctx, `SELECT d.path FROM streams st JOIN downloaded_subtitles d ON d.path = st.external_path WHERE st.id = ?`, streamID).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("only downloaded subtitles can be removed")
	}
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM streams WHERE id = ?`, streamID); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM downloaded_subtitles WHERE path = ?`, path); err != nil {
		return err
	}
	os.Remove(path)
	return nil
}

var iso6392 = map[string]string{
	"en": "eng", "es": "spa", "fr": "fre", "de": "ger", "it": "ita", "pt": "por", "pt-br": "por", "pt-pt": "por",
	"nl": "dut", "sv": "swe", "no": "nor", "da": "dan", "fi": "fin", "pl": "pol", "cs": "cze", "hu": "hun", "ro": "rum",
	"el": "gre", "tr": "tur", "ru": "rus", "uk": "ukr", "ar": "ara", "he": "heb", "hi": "hin", "ja": "jpn", "ko": "kor",
	"zh": "chi", "zh-cn": "chi", "zh-tw": "chi", "th": "tha", "vi": "vie", "id": "ind", "ms": "may", "hr": "hrv",
	"sr": "srp", "sl": "slv", "sk": "slo", "bg": "bul", "et": "est", "lv": "lav", "lt": "lit", "fa": "per", "bn": "ben",
}

// ISO6392 turns OpenSubtitles' language codes into the three-letter codes streams use.
func ISO6392(code string) string {
	if c, ok := iso6392[strings.ToLower(code)]; ok {
		return c
	}
	return strings.ToLower(code)
}
