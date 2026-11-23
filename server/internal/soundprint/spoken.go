package soundprint

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ---------- Spoken language (PLAY-19) ----------
//
// Audio tracks of films and episodes whose files don't say what language they're in are heard
// by the sidecar (Whisper's language identification); a clear answer becomes the track's
// language, marked as detected, so track menus, remembered choices (PLAY-8) and the HLS
// renditions show it. A file is heard once while its size and time stay the same.

// SpokenConfidence is the least share of Whisper's odds, averaged over the stretches heard,
// for a detected language to be used.
const SpokenConfidence = 0.7

// spokenAttempts is how many runs try a track the sidecar fails on its own before it's left
// alone (until its file or the model changes).
const spokenAttempts = 3

// SpokenResult is one track's detected language (ISO 639-1, "" when nothing clear).
type SpokenResult struct {
	ID         string  `json:"id"`
	Language   string  `json:"language"`
	Confidence float64 `json:"confidence"`
	Heard      int     `json:"heard"`
	Agree      int     `json:"agree"` // stretches whose own most likely language it is
	Error      string  `json:"error"`
}

// SpokenClips is one track's stretches to hear: 16-bit little-endian mono PCM at SpeechRate.
type SpokenClips struct {
	ID    string   `json:"id"`
	Clips [][]byte `json:"clips"` // sent as base64
}

// SpokenLanguage hears up to 16 tracks' stretches, cut by the server so the sidecar needn't see
// the files (it's given only the music folder).
func (c *Client) SpokenLanguage(ctx context.Context, items []SpokenClips) (string, []SpokenResult, error) {
	var out struct {
		Model   string         `json:"model"`
		Results []SpokenResult `json:"results"`
	}
	err := c.post(ctx, "/spoken_language_clips", map[string]any{"items": items}, &out)
	return out.Model, out.Results, err
}

// What the sidecar hears of a track: three stretches of speechClip seconds at these shares of
// its length (fewer when it's short), at SpeechRate. The same as soundprint/speech.py.
const (
	SpeechRate = 16000
	speechClip = 20.0
)

var speechSpots = []float64{0.25, 0.5, 0.75}

func speechStarts(durationSec float64) []float64 {
	if durationSec <= speechClip {
		return []float64{0}
	}
	if durationSec <= speechClip*float64(len(speechSpots)) {
		var out []float64
		for i := 0; float64(i+1)*speechClip <= durationSec; i++ {
			out = append(out, float64(i)*speechClip)
		}
		return out
	}
	out := make([]float64, len(speechSpots))
	for i, f := range speechSpots {
		out[i] = max(0, durationSec*f-speechClip/2)
	}
	return out
}

// speechVersion is the sidecar's spoken-language analysis version ("model#3" → 3): from 3 it
// takes stretches the server sends.
func speechVersion(id string) int {
	i := strings.LastIndexByte(id, '#')
	if i < 0 {
		return 0
	}
	n, _ := strconv.Atoi(id[i+1:])
	return n
}

// cutSpeech cuts a track's stretches with FFmpeg: one audio stream of the file, mono, at
// SpeechRate. Stretches that can't be read are left out; an error only when none could be.
func (s *Service) cutSpeech(ctx context.Context, path string, stream int, durationMS int64) ([][]byte, error) {
	var clips [][]byte
	var lastErr error
	for _, start := range speechStarts(float64(durationMS) / 1000) {
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		cmd := exec.CommandContext(cctx, s.FFmpeg, "-nostdin", "-v", "error", "-ss", strconv.FormatFloat(start, 'f', 2, 64),
			"-t", strconv.FormatFloat(speechClip, 'f', 2, 64), "-i", path, "-map", "0:"+strconv.Itoa(stream),
			"-vn", "-ac", "1", "-ar", strconv.Itoa(SpeechRate), "-f", "s16le", "pipe:1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			lastErr = fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
			continue
		}
		if len(out) > 0 {
			clips = append(clips, out)
		}
	}
	if len(clips) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return clips, nil
}

// spokenPass hears the audio tracks of films, episodes and videos that have no language and
// haven't been heard by this model since their file last changed. A file FFmpeg can't read is
// kept as heard with no language, until it changes. A track the sidecar fails on its own is
// tried again on the next runs, spokenAttempts times in all; when it fails everything it was
// sent the fault is the sidecar's, so nothing is counted and the run stops.
func (s *Service) spokenPass(ctx context.Context, h Health) (string, error) {
	if speechVersion(h.Speech) < 3 || s.FFmpeg == "" {
		return "", nil
	}
	type todo struct {
		fileID, size, mtime, durationMS int64
		index                           int
		path                            string
	}
	var list []todo
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id, f.size, f.mtime, COALESCE(f.duration_ms, 0), st.stream_index, f.path FROM streams st
		JOIN media_files f ON f.id = st.file_id AND f.available = 1
		JOIN media_versions v ON v.id = f.version_id
		JOIN items i ON i.id = v.item_id AND i.type IN ('movie', 'episode', 'video') AND i.extra_type IS NULL
		WHERE st.kind = 'audio' AND COALESCE(st.language, '') IN ('', 'und') AND st.stream_index IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM spoken_languages d WHERE d.file_id = f.id AND d.stream_index = st.stream_index
			AND d.size = f.size AND d.mtime = f.mtime AND d.model = ? AND (d.failures = 0 OR d.failures >= ?))
		ORDER BY i.added_at DESC, f.id, st.stream_index`, h.Speech, spokenAttempts)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var t todo
		if err := rows.Scan(&t.fileID, &t.size, &t.mtime, &t.durationMS, &t.index, &t.path); err != nil {
			rows.Close()
			return "", err
		}
		list = append(list, t)
	}
	rows.Close()
	if len(list) == 0 {
		return "", nil
	}
	s.setPhase("Hearing which language audio tracks are in", len(list))
	start := time.Now()
	found, unclear, unreadable, failed := 0, 0, 0, 0
	firstErr := ""
	record := func(t todo, lang string, confidence float64, model string) error {
		_, err := s.DB.ExecContext(ctx, `INSERT INTO spoken_languages (file_id, stream_index, size, mtime, language, confidence, model)
			VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?)
			ON CONFLICT (file_id, stream_index) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, language = excluded.language,
			confidence = excluded.confidence, model = excluded.model, failures = 0, checked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
			t.fileID, t.index, t.size, t.mtime, lang, confidence, model)
		return err
	}
	// A try the sidecar failed: counted while the file and the model are the same, else the first.
	recordFailure := func(t todo) error {
		_, err := s.DB.ExecContext(ctx, `INSERT INTO spoken_languages (file_id, stream_index, size, mtime, model, failures)
			VALUES (?, ?, ?, ?, ?, 1)
			ON CONFLICT (file_id, stream_index) DO UPDATE SET
			failures = CASE WHEN size = excluded.size AND mtime = excluded.mtime AND model = excluded.model THEN failures + 1 ELSE 1 END,
			size = excluded.size, mtime = excluded.mtime, language = NULL, confidence = 0, model = excluded.model,
			checked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
			t.fileID, t.index, t.size, t.mtime, h.Speech)
		return err
	}
	const batch = 4
	for i := 0; i < len(list); i += batch {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !s.Enabled() {
			break
		}
		chunk := list[i:min(i+batch, len(list))]
		var items []SpokenClips
		var sent []todo
		for _, t := range chunk {
			clips, err := s.cutSpeech(ctx, t.path, t.index, t.durationMS)
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if err != nil {
				unreadable++
				if firstErr == "" {
					firstErr = "reading " + t.path + ": " + err.Error()
				}
				if err := record(t, "", 0, h.Speech); err != nil {
					return "", err
				}
				continue
			}
			items = append(items, SpokenClips{ID: strconv.Itoa(len(items)), Clips: clips})
			sent = append(sent, t)
		}
		if len(items) > 0 {
			model, results, err := s.Client.SpokenLanguage(ctx, items)
			if err != nil {
				return "", fmt.Errorf("spoken language after %d of %d tracks: %w", i, len(list), err)
			}
			// Answers are matched to tracks by the id sent, not by their place in the list; a
			// track with no answer counts as one the sidecar failed.
			byID := map[string]SpokenResult{}
			for _, r := range results {
				byID[r.ID] = r
			}
			var failedTracks []todo
			sidecarErr := ""
			for j, t := range sent {
				r, ok := byID[items[j].ID]
				switch {
				case !ok:
					r.Error = "no answer for the track"
					fallthrough
				case r.Error != "":
					failedTracks = append(failedTracks, t)
					sidecarErr = r.Error
					if firstErr == "" {
						firstErr = "Soundprint: " + r.Error
					}
				case r.Language != "" && r.Agree >= 2 && r.Confidence >= SpokenConfidence:
					lang := SpokenCode(r.Language)
					if err := record(t, lang, r.Confidence, model); err != nil {
						return "", err
					}
					if _, err := s.DB.ExecContext(ctx, `UPDATE streams SET language = ?, language_detected = 1
						WHERE file_id = ? AND stream_index = ? AND kind = 'audio' AND COALESCE(language, '') IN ('', 'und')`, lang, t.fileID, t.index); err != nil {
						return "", err
					}
					found++
				default:
					unclear++
					if err := record(t, "", r.Confidence, model); err != nil {
						return "", err
					}
				}
			}
			failed += len(failedTracks)
			// The sidecar failed every one of several tracks: the fault is its own, so the tries
			// aren't counted against the tracks, and the run stops rather than go through the rest.
			if len(failedTracks) == len(sent) && len(sent) > 1 {
				slog.Warn("spoken languages stopped", "error", sidecarErr)
				return "", fmt.Errorf("Soundprint couldn't hear the audio tracks: %s", sidecarErr)
			}
			for _, t := range failedTracks {
				if err := recordFailure(t); err != nil {
					return "", err
				}
			}
			// A track sent alone that failed can't be told from a failing sidecar: its try is
			// counted (so it isn't tried for ever), and the run still stops.
			if len(failedTracks) == len(sent) {
				slog.Warn("spoken languages stopped", "error", sidecarErr)
				return "", fmt.Errorf("Soundprint couldn't hear the audio tracks: %s", sidecarErr)
			}
		}
		s.setDone(min(i+batch, len(list)))
	}
	slog.Info("spoken languages heard", "tracks", len(list), "found", found, "unclear", unclear, "unreadable", unreadable,
		"failed", failed, "took", time.Since(start).Round(time.Second))
	if firstErr != "" {
		slog.Warn("spoken languages: some tracks couldn't be heard", "first", firstErr)
	}
	if found == 0 {
		return "", nil
	}
	return fmt.Sprintf("found the language of %d audio tracks", found), nil
}

// SpokenCode turns Whisper's ISO 639-1 code into the ISO 639-2 (bibliographic) code files
// use; a code it doesn't know is kept as it is.
func SpokenCode(l string) string {
	if c, ok := iso6392[l]; ok {
		return c
	}
	return l
}

var iso6392 = map[string]string{
	"af": "afr", "am": "amh", "ar": "ara", "as": "asm", "az": "aze", "ba": "bak", "be": "bel", "bg": "bul", "bn": "ben",
	"bo": "tib", "br": "bre", "bs": "bos", "ca": "cat", "cs": "cze", "cy": "wel", "da": "dan", "de": "ger", "el": "gre",
	"en": "eng", "es": "spa", "et": "est", "eu": "baq", "fa": "per", "fi": "fin", "fo": "fao", "fr": "fre", "gl": "glg",
	"gu": "guj", "ha": "hau", "haw": "haw", "he": "heb", "hi": "hin", "hr": "hrv", "ht": "hat", "hu": "hun", "hy": "arm",
	"id": "ind", "is": "ice", "it": "ita", "ja": "jpn", "jw": "jav", "ka": "geo", "kk": "kaz", "km": "khm", "kn": "kan",
	"ko": "kor", "la": "lat", "lb": "ltz", "ln": "lin", "lo": "lao", "lt": "lit", "lv": "lav", "mg": "mlg", "mi": "mao",
	"mk": "mac", "ml": "mal", "mn": "mon", "mr": "mar", "ms": "may", "mt": "mlt", "my": "bur", "ne": "nep", "nl": "dut",
	"nn": "nno", "no": "nor", "oc": "oci", "pa": "pan", "pl": "pol", "ps": "pus", "pt": "por", "ro": "rum", "ru": "rus",
	"sa": "san", "sd": "snd", "si": "sin", "sk": "slo", "sl": "slv", "sn": "sna", "so": "som", "sq": "alb", "sr": "srp",
	"su": "sun", "sv": "swe", "sw": "swa", "ta": "tam", "te": "tel", "tg": "tgk", "th": "tha", "tk": "tuk", "tl": "tgl",
	"tr": "tur", "tt": "tat", "uk": "ukr", "ur": "urd", "uz": "uzb", "vi": "vie", "yi": "yid", "yo": "yor", "yue": "chi",
	"zh": "chi",
}
