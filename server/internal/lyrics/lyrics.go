// Package lyrics finds a track's lyrics (MUSIC-10): tags embedded in the file first, then
// an .lrc sidecar next to it, then LRCLIB (lrclib.net) when online lookups are enabled.
// Results, including "none found", are cached in the lyrics table.
package lyrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrNone = errors.New("no lyrics found")

type Line struct {
	TimeMS int64 // -1 when not timed
	Text   string
}

type Lyrics struct {
	Synced bool
	Source string // embedded, sidecar, lrclib
	Lines  []Line
}

type Service struct {
	DB     *sql.DB
	HTTP   *http.Client
	Online func() bool
	// BaseURL is LRCLIB's API (overridable in tests).
	BaseURL string
}

var lrcTime = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)

// Parse reads LRC (timed) or plain text lyrics.
func Parse(text string) (lines []Line, synced bool) {
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		stamps := lrcTime.FindAllStringSubmatchIndex(raw, -1)
		if len(stamps) == 0 {
			if strings.HasPrefix(strings.TrimSpace(raw), "[") && strings.Contains(raw, ":") && strings.HasSuffix(strings.TrimSpace(raw), "]") {
				continue // LRC metadata like [ar:Artist]
			}
			lines = append(lines, Line{TimeMS: -1, Text: strings.TrimSpace(raw)})
			continue
		}
		synced = true
		textPart := strings.TrimSpace(raw[stamps[len(stamps)-1][1]:])
		for _, st := range stamps {
			m, _ := strconv.Atoi(raw[st[2]:st[3]])
			s, _ := strconv.Atoi(raw[st[4]:st[5]])
			frac := 0
			if st[6] >= 0 {
				f := raw[st[6]:st[7]]
				frac, _ = strconv.Atoi(f)
				switch len(f) {
				case 1:
					frac *= 100
				case 2:
					frac *= 10
				}
			}
			lines = append(lines, Line{TimeMS: int64(m*60_000 + s*1000 + frac), Text: textPart})
		}
	}
	if synced {
		// Keep timed lines only, in order (some files mix in untimed credits).
		var timed []Line
		for _, l := range lines {
			if l.TimeMS >= 0 {
				timed = append(timed, l)
			}
		}
		for i := 1; i < len(timed); i++ {
			for j := i; j > 0 && timed[j].TimeMS < timed[j-1].TimeMS; j-- {
				timed[j], timed[j-1] = timed[j-1], timed[j]
			}
		}
		lines = timed
	}
	// Trim blank lines at the ends.
	for len(lines) > 0 && lines[0].Text == "" && lines[0].TimeMS < 0 {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1].Text == "" && lines[len(lines)-1].TimeMS < 0 {
		lines = lines[:len(lines)-1]
	}
	return lines, synced
}

type track struct {
	path, probe, title, artist, album string
	durationMS                        int64
}

// Get returns a track's lyrics, looking them up once and caching the result.
func (s *Service) Get(ctx context.Context, itemID int64) (Lyrics, error) {
	var text, source string
	var synced bool
	var fetched string
	err := s.DB.QueryRowContext(ctx, `SELECT text, source, synced, fetched_at FROM lyrics WHERE item_id = ?`, itemID).Scan(&text, &source, &synced, &fetched)
	if err == nil {
		at, _ := time.Parse(time.RFC3339Nano, fetched)
		// "None found" is retried after a month (LRCLIB grows), or when online lookups get turned on.
		stale := text == "" && (time.Since(at) > 30*24*time.Hour || (source != "lrclib" && s.Online()))
		if !stale {
			if text == "" {
				return Lyrics{}, ErrNone
			}
			lines, sy := Parse(text)
			return Lyrics{Synced: sy, Source: source, Lines: lines}, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Lyrics{}, err
	}

	var t track
	err = s.DB.QueryRowContext(ctx, `SELECT f.path, COALESCE(f.probe_json, ''), i.title, COALESCE(i.artist_credit, g.title, ''), COALESCE(p.title, ''),
		COALESCE(i.duration_ms, 0) FROM items i JOIN media_versions v ON v.item_id = i.id JOIN media_files f ON f.version_id = v.id
		LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id
		WHERE i.id = ? AND i.type = 'track' LIMIT 1`, itemID).Scan(&t.path, &t.probe, &t.title, &t.artist, &t.album, &t.durationMS)
	if errors.Is(err, sql.ErrNoRows) {
		return Lyrics{}, ErrNone
	}
	if err != nil {
		return Lyrics{}, err
	}

	text, source = embedded(t.probe), "embedded"
	if text == "" {
		text, source = sidecar(t.path), "sidecar"
	}
	if text == "" && s.Online() {
		text, source = s.lrclib(ctx, t), "lrclib"
	}
	if text == "" && !s.Online() {
		source = "embedded" // remember that only local sources were tried
	}
	lines, sy := Parse(text)
	s.DB.ExecContext(ctx, `INSERT INTO lyrics(item_id, synced, text, source) VALUES (?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET synced = excluded.synced, text = excluded.text, source = excluded.source,
		fetched_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, itemID, sy, text, source)
	if text == "" || len(lines) == 0 {
		return Lyrics{}, ErrNone
	}
	return Lyrics{Synced: sy, Source: source, Lines: lines}, nil
}

// embedded reads lyrics tags from ffprobe output (format or stream tags, any case).
func embedded(probeJSON string) string {
	if probeJSON == "" {
		return ""
	}
	var p struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Tags map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal([]byte(probeJSON), &p) != nil {
		return ""
	}
	maps := []map[string]string{p.Format.Tags}
	for _, st := range p.Streams {
		maps = append(maps, st.Tags)
	}
	for _, tags := range maps {
		for k, v := range tags {
			switch strings.ToLower(k) {
			case "lyrics", "unsyncedlyrics", "uslt", "lyrics-eng", "syncedlyrics":
				if strings.TrimSpace(v) != "" {
					return v
				}
			}
			if strings.HasPrefix(strings.ToLower(k), "lyrics") && strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	return ""
}

// sidecar reads Song.lrc (or Song.txt) next to the audio file.
func sidecar(path string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range []string{".lrc", ".LRC", ".txt"} {
		if b, err := os.ReadFile(base + ext); err == nil && len(b) < 1<<20 {
			return string(b)
		}
	}
	return ""
}

func (s *Service) lrclib(ctx context.Context, t track) string {
	base := s.BaseURL
	if base == "" {
		base = "https://lrclib.net"
	}
	q := url.Values{"track_name": {t.title}, "artist_name": {t.artist}}
	if t.album != "" {
		q.Set("album_name", t.album)
	}
	if t.durationMS > 0 {
		q.Set("duration", strconv.FormatInt((t.durationMS+500)/1000, 10))
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/get?"+q.Encode(), nil)
	req.Header.Set("User-Agent", "Marquee (self-hosted media server)")
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var r struct {
		Synced string `json:"syncedLyrics"`
		Plain  string `json:"plainLyrics"`
	}
	if json.NewDecoder(resp.Body).Decode(&r) != nil {
		return ""
	}
	if r.Synced != "" {
		return r.Synced
	}
	return r.Plain
}
