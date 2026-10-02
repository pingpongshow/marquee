package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

func statFollow(p string) (os.FileInfo, error) { return os.Stat(p) }

type subtitleFile struct {
	Path            string
	Codec           string
	Language        string
	Title           string
	Forced          bool
	HearingImpaired bool
	Default         bool
}

var subCodecs = map[string]string{
	"srt": "subrip", "ass": "ass", "ssa": "ssa", "vtt": "webvtt",
	"idx": "dvd_subtitle", "sup": "hdmv_pgs_subtitle", "smi": "sami",
}

// Two-letter and English-name language tags → ISO 639-2/B (the form ffprobe reports).
var langCodes = map[string]string{
	"en": "eng", "english": "eng", "es": "spa", "spanish": "spa", "fr": "fre", "french": "fre",
	"de": "ger", "german": "ger", "it": "ita", "italian": "ita", "pt": "por", "portuguese": "por",
	"ja": "jpn", "japanese": "jpn", "jp": "jpn", "ko": "kor", "korean": "kor", "zh": "chi", "chinese": "chi",
	"ru": "rus", "russian": "rus", "nl": "dut", "dutch": "dut", "sv": "swe", "swedish": "swe",
	"no": "nor", "nb": "nor", "da": "dan", "fi": "fin", "pl": "pol", "tr": "tur", "ar": "ara",
	"he": "heb", "hi": "hin", "th": "tha", "vi": "vie", "id": "ind", "cs": "cze", "el": "gre",
	"hu": "hun", "ro": "rum", "uk": "ukr", "ms": "may", "tl": "tgl", "fil": "fil",
}

var knownISO3 = set("eng", "spa", "fre", "fra", "ger", "deu", "ita", "por", "jpn", "kor", "chi", "zho", "rus",
	"dut", "nld", "swe", "nor", "dan", "fin", "pol", "tur", "ara", "heb", "hin", "tha", "vie", "ind", "cze",
	"ces", "gre", "ell", "hun", "rum", "ron", "ukr", "may", "msa", "tgl", "fil", "und")

// findSubtitles returns sidecar subtitle files for a video: files in the same directory
// named "<video base>.<tags>.<ext>", e.g. "Movie.en.forced.srt", "Show S01E01.en.dub.srt".
func findSubtitles(videoPath string, dirFiles []string) []subtitleFile {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	lowerBase := strings.ToLower(base)
	var out []subtitleFile
	for _, name := range dirFiles {
		e := ext(name)
		codec, ok := subCodecs[e]
		if !ok {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if !strings.EqualFold(stem, base) && !strings.HasPrefix(strings.ToLower(stem), lowerBase+".") {
			continue
		}
		sf := subtitleFile{Path: filepath.Join(dir, name), Codec: codec}
		var extra []string
		if len(stem) > len(base) {
			for _, tok := range strings.Split(stem[len(base)+1:], ".") {
				t := strings.ToLower(strings.TrimSpace(tok))
				switch {
				case t == "":
				case t == "forced" || t == "foreign":
					sf.Forced = true
				case t == "sdh" || t == "cc" || t == "hi":
					sf.HearingImpaired = true
				case t == "default":
					sf.Default = true
				case sf.Language == "" && langCodes[t] != "":
					sf.Language = langCodes[t]
				case sf.Language == "" && knownISO3[t]:
					sf.Language = t
				default:
					extra = append(extra, tok)
				}
			}
		}
		if len(extra) > 0 {
			sf.Title = strings.Join(extra, " ")
		}
		out = append(out, sf)
	}
	return out
}

// findSidecar returns the first file in dirFiles named "<base>.<ext>" for one of exts.
func findSidecar(mediaPath string, dirFiles []string, exts ...string) string {
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	for _, name := range dirFiles {
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if !strings.EqualFold(stem, base) {
			continue
		}
		for _, e := range exts {
			if ext(name) == e {
				return filepath.Join(filepath.Dir(mediaPath), name)
			}
		}
	}
	return ""
}

// findArtwork looks for local artwork in dir: names are checked in priority order,
// with any image extension.
func findArtwork(dir string, dirFiles []string, names ...string) string {
	for _, want := range names {
		for _, name := range dirFiles {
			if imageExts[ext(name)] && strings.EqualFold(strings.TrimSuffix(name, filepath.Ext(name)), want) {
				return filepath.Join(dir, name)
			}
		}
	}
	return ""
}
