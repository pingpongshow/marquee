// Package sublang finds the language of subtitle tracks whose file doesn't say (PLAY-20), from
// their text: three stretches of it (the start, middle and end of a subtitle file; three
// two-minute stretches of a track inside a video, so the whole file isn't read), each
// identified on its own. A language counts only when at least two stretches agree on it,
// reliably, and the whole text says the same. Picture subtitles (Blu-ray PGS, DVD VobSub) have
// no text and stay unknown.
//
// A clear answer becomes the track's language, marked as detected, so track menus, remembered
// choices (PLAY-8) and the players show it. A track is read once while its file is the same.
package sublang

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/abadojack/whatlanggo"
)

// Model names the reading in use: tracks read by another are read again.
const Model = "whatlanggo#1"

const (
	window = 120.0 // seconds of an embedded track read at each of the three places
	agree  = 2     // stretches that must agree on the language
)

var spots = []float64{0.25, 0.5, 0.75}

// pictureCodecs are subtitle codecs with no text.
var pictureCodecs = map[string]bool{"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true, "dvb_teletext": true}

// Service reads subtitle tracks' languages.
type Service struct {
	DB     *sql.DB
	FFmpeg string

	done, total atomic.Int64
}

// Progress reports the current run's progress.
func (s *Service) Progress() (done, total int) { return int(s.done.Load()), int(s.total.Load()) }

type track struct {
	streamID, fileID int64
	index            sql.NullInt64
	codec, external  string
	path             string
	size, mtime      int64
	durationMS       int64
	key              string
}

// Run reads the tracks without a language that haven't been read since their file changed.
func (s *Service) Run(ctx context.Context) (string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT st.id, f.id, st.stream_index, st.codec, COALESCE(st.external_path, ''), f.path, f.size, f.mtime,
			COALESCE(f.duration_ms, 0)
		FROM streams st JOIN media_files f ON f.id = st.file_id AND f.available = 1
		JOIN media_versions v ON v.id = f.version_id
		JOIN items i ON i.id = v.item_id AND i.type IN ('movie', 'episode', 'video') AND i.extra_type IS NULL
		WHERE st.kind = 'subtitle' AND COALESCE(st.language, '') IN ('', 'und')
		ORDER BY i.added_at DESC, f.id, st.id`)
	if err != nil {
		return "", err
	}
	var todo []track
	for rows.Next() {
		var t track
		if err := rows.Scan(&t.streamID, &t.fileID, &t.index, &t.codec, &t.external, &t.path, &t.size, &t.mtime, &t.durationMS); err != nil {
			rows.Close()
			return "", err
		}
		if pictureCodecs[t.codec] {
			continue
		}
		todo = append(todo, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	// A subtitle file is keyed by its own path, size and time; an embedded track by its index
	// and the video file's.
	var pending []track
	for _, t := range todo {
		switch {
		case t.external != "":
			fi, err := os.Stat(t.external)
			if err != nil {
				continue
			}
			t.key, t.size, t.mtime = "x:"+t.external, fi.Size(), fi.ModTime().Unix()
		case t.index.Valid:
			t.key = "s:" + strconv.FormatInt(t.index.Int64, 10)
		default:
			continue
		}
		var n int
		s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_languages WHERE file_id = ? AND stream_key = ? AND size = ? AND mtime = ? AND model = ?`,
			t.fileID, t.key, t.size, t.mtime, Model).Scan(&n)
		if n == 0 {
			pending = append(pending, t)
		}
	}
	if len(pending) == 0 {
		return "No subtitles to read", nil
	}
	s.total.Store(int64(len(pending)))
	s.done.Store(0)
	start := time.Now()
	found, unclear, unreadable := 0, 0, 0
	for i, t := range pending {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var parts []string
		var err error
		if t.external != "" {
			parts, err = s.readFile(t.external)
		} else {
			parts, err = s.readEmbedded(ctx, t)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		lang := ""
		if err != nil {
			unreadable++
			slog.Debug("subtitle language: unreadable", "path", t.path, "key", t.key, "err", err)
		} else if lang = Identify(parts); lang == "" {
			unclear++
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO subtitle_languages (file_id, stream_key, size, mtime, language, model) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?)
			ON CONFLICT (file_id, stream_key) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, language = excluded.language,
			model = excluded.model, checked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, t.fileID, t.key, t.size, t.mtime, lang, Model); err != nil {
			return "", err
		}
		if lang != "" {
			if _, err := s.DB.ExecContext(ctx, `UPDATE streams SET language = ?, language_detected = 1 WHERE id = ? AND COALESCE(language, '') IN ('', 'und')`,
				lang, t.streamID); err != nil {
				return "", err
			}
			found++
		}
		s.done.Store(int64(i + 1))
	}
	slog.Info("subtitle languages read", "tracks", len(pending), "found", found, "unclear", unclear, "unreadable", unreadable,
		"took", time.Since(start).Round(time.Second))
	return fmt.Sprintf("Found the language of %d of %d subtitle tracks", found, len(pending)), nil
}

// readFile reads a subtitle file's text, split into its start, middle and end.
func (s *Service) readFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, 4<<20)
	n, _ := f.Read(b)
	lines := Dialogue(b[:n])
	if len(lines) == 0 {
		return nil, nil
	}
	third := (len(lines) + 2) / 3
	var parts []string
	for i := 0; i < len(lines); i += third {
		parts = append(parts, strings.Join(lines[i:min(i+third, len(lines))], " "))
	}
	return parts, nil
}

// readEmbedded reads three two-minute stretches of a track inside a video, as SubRip.
func (s *Service) readEmbedded(ctx context.Context, t track) ([]string, error) {
	starts := []float64{0}
	if d := float64(t.durationMS) / 1000; d > window*3 {
		starts = starts[:0]
		for _, f := range spots {
			starts = append(starts, max(0, d*f-window/2))
		}
	} else if d > window {
		starts = []float64{0, window}
	}
	var parts []string
	var lastErr error
	for _, at := range starts {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		cmd := exec.CommandContext(cctx, s.FFmpeg, "-nostdin", "-v", "error", "-ss", strconv.FormatFloat(at, 'f', 2, 64),
			"-t", strconv.FormatFloat(window, 'f', 2, 64), "-i", t.path, "-map", "0:"+strconv.FormatInt(t.index.Int64, 10),
			"-c:s", "srt", "-f", "srt", "pipe:1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
			continue
		}
		if text := strings.Join(Dialogue(out), " "); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return parts, nil
}

var (
	tags      = regexp.MustCompile(`<[^>]*>|\{[^}]*\}`)
	timing    = regexp.MustCompile(`-->`)
	cueNumber = regexp.MustCompile(`^\d+$`)
)

// Dialogue is the spoken lines of SubRip, WebVTT or ASS/SSA text, without numbers, timings,
// styles and tags.
func Dialogue(b []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		switch {
		case line == "", cueNumber.MatchString(line), timing.MatchString(line), strings.HasPrefix(line, "WEBVTT"),
			strings.HasPrefix(line, "NOTE"), strings.HasPrefix(line, "STYLE"):
			continue
		case strings.HasPrefix(line, "Dialogue:"):
			// ASS: the text is the tenth field.
			f := strings.SplitN(line, ",", 10)
			if len(f) < 10 {
				continue
			}
			line = strings.NewReplacer(`\N`, " ", `\n`, " ", `\h`, " ").Replace(f[9])
		case strings.HasPrefix(line, "[") || strings.Contains(line, ":") && isASSHeader(line):
			continue
		}
		line = strings.TrimSpace(tags.ReplaceAllString(line, ""))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func isASSHeader(line string) bool {
	for _, p := range []string{"Format:", "Style:", "Title:", "ScriptType:", "PlayResX:", "PlayResY:", "WrapStyle:", "ScaledBorderAndShadow:",
		"YCbCr Matrix:", "Collisions:", "Timer:", "Comment:", "Original", "Update Details:", "Synch Point:", "Kerning:", "Language:"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// Identify is the language (ISO 639-2) the stretches agree on, "" when it isn't clear. Every
// stretch with enough text names the language it reads most like. When they all name the same
// one (at least two of them), and so does the whole text, that's the language, even if the
// reading isn't sure of it: Russian, say, reads close to Ukrainian and Belarusian, so it's never
// "reliable" on its own. When they differ, at least two must name the same language reliably,
// and the whole text must reliably say it too.
func Identify(parts []string) string {
	all, sure := map[string]int{}, map[string]int{}
	for _, p := range parts {
		if len([]rune(p)) < 40 {
			continue
		}
		info := whatlanggo.Detect(p)
		l := info.Lang.Iso6393()
		if l == "" {
			continue
		}
		all[l]++
		if info.IsReliable() {
			sure[l]++
		}
	}
	whole := whatlanggo.Detect(strings.Join(parts, " "))
	wholeLang := whole.Lang.Iso6393()
	if len(all) == 1 {
		for l, n := range all {
			if n >= agree && l == wholeLang {
				return Code(l)
			}
		}
		return ""
	}
	for l, n := range sure {
		if n >= agree && l == wholeLang && whole.IsReliable() {
			return Code(l)
		}
	}
	return ""
}

// Code turns an ISO 639-3 code into the ISO 639-2 (bibliographic) code files use.
func Code(l string) string {
	if c, ok := bibliographic[l]; ok {
		return c
	}
	return l
}

var bibliographic = map[string]string{
	"fra": "fre", "deu": "ger", "nld": "dut", "cmn": "chi", "zho": "chi", "arb": "ara", "ces": "cze", "slk": "slo", "ron": "rum",
	"ell": "gre", "pes": "per", "fas": "per", "zsm": "may", "msa": "may", "nob": "nor", "isl": "ice", "eus": "baq", "hye": "arm",
	"kat": "geo", "mkd": "mac", "mya": "bur", "sqi": "alb", "cym": "wel", "ydd": "yid", "azj": "aze", "npi": "nep", "swh": "swa",
	"uzn": "uzb", "ekk": "est", "lvs": "lav", "plt": "mlg", "khk": "mon", "tgl": "tgl", "bod": "tib",
}
