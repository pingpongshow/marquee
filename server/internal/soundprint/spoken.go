package soundprint

import (
	"context"
	"fmt"
	"log/slog"
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

// SpokenResult is one audio stream's detected language (ISO 639-1, "" when nothing clear).
type SpokenResult struct {
	Path       string  `json:"path"`
	Stream     int     `json:"stream"`
	Language   string  `json:"language"`
	Confidence float64 `json:"confidence"`
	Heard      int     `json:"heard"`
	Agree      int     `json:"agree"` // stretches whose own most likely language it is
	Error      string  `json:"error"`
}

// SpokenItem is an audio stream to hear: the file and the stream's index in it.
type SpokenItem struct {
	Path   string `json:"path"`
	Stream int    `json:"stream"`
}

// SpokenLanguage hears up to 16 audio streams.
func (c *Client) SpokenLanguage(ctx context.Context, items []SpokenItem) (string, []SpokenResult, error) {
	var out struct {
		Model   string         `json:"model"`
		Results []SpokenResult `json:"results"`
	}
	err := c.post(ctx, "/spoken_language", map[string]any{"items": items}, &out)
	return out.Model, out.Results, err
}

// spokenPass hears the audio tracks of films, episodes and videos that have no language and
// haven't been heard by this model since their file last changed.
func (s *Service) spokenPass(ctx context.Context, h Health) (string, error) {
	if h.Speech == "" {
		return "", nil
	}
	type todo struct {
		fileID, size, mtime int64
		index               int
		path                string
	}
	var list []todo
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id, f.size, f.mtime, st.stream_index, f.path FROM streams st
		JOIN media_files f ON f.id = st.file_id AND f.available = 1
		JOIN media_versions v ON v.id = f.version_id
		JOIN items i ON i.id = v.item_id AND i.type IN ('movie', 'episode', 'video') AND i.extra_type IS NULL
		WHERE st.kind = 'audio' AND COALESCE(st.language, '') IN ('', 'und') AND st.stream_index IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM spoken_languages d WHERE d.file_id = f.id AND d.stream_index = st.stream_index
			AND d.size = f.size AND d.mtime = f.mtime AND d.model = ?)
		ORDER BY i.added_at DESC, f.id, st.stream_index`, h.Speech)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var t todo
		if err := rows.Scan(&t.fileID, &t.size, &t.mtime, &t.index, &t.path); err != nil {
			rows.Close()
			return "", err
		}
		list = append(list, t)
	}
	rows.Close()
	if err != nil || len(list) == 0 {
		return "", err
	}
	s.setPhase("Hearing which language audio tracks are in", len(list))
	start := time.Now()
	found, unclear, failed := 0, 0, 0
	const batch = 8
	for i := 0; i < len(list); i += batch {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !s.Enabled() {
			break
		}
		chunk := list[i:min(i+batch, len(list))]
		items := make([]SpokenItem, len(chunk))
		for j, t := range chunk {
			items[j] = SpokenItem{Path: t.path, Stream: t.index}
		}
		model, results, err := s.Client.SpokenLanguage(ctx, items)
		if err != nil {
			return "", fmt.Errorf("spoken language after %d tracks: %w", i, err)
		}
		for j, r := range results {
			if j >= len(chunk) {
				break
			}
			t := chunk[j]
			lang := ""
			switch {
			case r.Error != "":
				failed++ // kept with no language, so it isn't retried every run
			case r.Language != "" && r.Confidence >= SpokenConfidence:
				lang = SpokenCode(r.Language)
			default:
				unclear++
			}
			if _, err := s.DB.ExecContext(ctx, `INSERT INTO spoken_languages (file_id, stream_index, size, mtime, language, confidence, model)
				VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?)
				ON CONFLICT (file_id, stream_index) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, language = excluded.language,
				confidence = excluded.confidence, model = excluded.model, checked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
				t.fileID, t.index, t.size, t.mtime, lang, r.Confidence, model); err != nil {
				return "", err
			}
			if lang != "" {
				if _, err := s.DB.ExecContext(ctx, `UPDATE streams SET language = ?, language_detected = 1
					WHERE file_id = ? AND stream_index = ? AND kind = 'audio' AND COALESCE(language, '') IN ('', 'und')`, lang, t.fileID, t.index); err != nil {
					return "", err
				}
				found++
			}
		}
		s.setDone(min(i+batch, len(list)))
	}
	slog.Info("spoken languages heard", "tracks", len(list), "found", found, "unclear", unclear, "failed", failed,
		"took", time.Since(start).Round(time.Second))
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
