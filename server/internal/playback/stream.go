package playback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Media URLs live under /api/v1/stream/{session}/… and are fetched directly by players
// (<video>, hls.js, AVPlayer), which can't always send auth headers. The session id is an
// unguessable capability that stops working when the session ends (WAN-5).
const StreamPrefix = "/api/v1/stream/"

func (m *Manager) StreamHandler(subtitleCache string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, StreamPrefix)
		id, file, ok := strings.Cut(rest, "/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		s, found := m.Get(id)
		if !found {
			http.Error(w, "playback session ended", http.StatusGone)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// Lower ABR variants live under r<n>/ (PLAY-15).
		rung := 0
		if strings.HasPrefix(file, "r") {
			if head, tail, ok := strings.Cut(file, "/"); ok {
				if n, err := strconv.Atoi(head[1:]); err == nil && n > 0 {
					rung, file = n, tail
				}
			}
		}
		switch {
		case file == "file":
			m.serveFile(w, r, s)
		case file == "master.m3u8":
			m.serveMaster(w, s)
		case file == "index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(PlanPlaylist(s.Plan, s.Media.DurationMS))
		case file == "init.mp4":
			m.serveFromTranscoder(w, r, s, rung, -1)
		case strings.HasSuffix(file, ".m4s"):
			k, err := strconv.Atoi(strings.TrimSuffix(file, ".m4s"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			m.serveFromTranscoder(w, r, s, rung, k)
		case strings.HasPrefix(file, "subtitles/") && strings.HasSuffix(file, ".m3u8"):
			sid, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(file, "subtitles/"), ".m3u8"), 10, 64)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			serveSubtitlePlaylist(w, s, sid)
		case strings.HasPrefix(file, "subtitles/") && (strings.HasSuffix(file, ".vtt") || strings.HasSuffix(file, ".ass")):
			name := strings.TrimPrefix(file, "subtitles/")
			format := strings.TrimPrefix(filepath.Ext(name), ".")
			sid, err := strconv.ParseInt(strings.TrimSuffix(name, "."+format), 10, 64)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			m.serveSubtitle(w, r, s, sid, subtitleCache, format)
		case file == "fonts.json":
			m.serveFontList(w, r, s)
		case strings.HasPrefix(file, "fonts/"):
			idx, err := strconv.Atoi(strings.TrimPrefix(file, "fonts/"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			m.serveFont(w, r, s, idx, subtitleCache)
		case file == "audio":
			m.serveAudioTranscode(w, r, s)
		default:
			http.NotFound(w, r)
		}
	})
}

func (m *Manager) serveFile(w http.ResponseWriter, r *http.Request, s *Session) {
	f, err := os.Open(s.Path)
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(s.Path))); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", contentTypeFor(s.Media.Container, s.Media.Video == nil))
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func contentTypeFor(container string, audio bool) string {
	switch normalizeContainer(container) {
	case "mp4":
		if audio {
			return "audio/mp4"
		}
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mkv":
		return "video/x-matroska"
	case "flac":
		return "audio/flac"
	case "mp3":
		return "audio/mpeg"
	case "ogg":
		return "audio/ogg"
	}
	return "application/octet-stream"
}

// codecsString builds the RFC 6381 CODECS attribute for the master playlist.
func codecsString(s *Session) string {
	var c []string
	d := s.Decision
	if v := s.Media.Video; v != nil {
		codec := v.Codec
		if !d.VideoCopy {
			codec = d.VideoCodec
		}
		switch codec {
		case "hevc":
			c = append(c, "hvc1.1.6.L150.90")
		case "av1":
			c = append(c, "av01.0.08M.08")
		default:
			c = append(c, "avc1.640029")
		}
	}
	if a := s.Media.Audio; a != nil {
		codec := a.Codec
		if !d.AudioCopy {
			codec = d.AudioCodec
		}
		switch codec {
		case "ac3":
			c = append(c, "ac-3")
		case "eac3":
			c = append(c, "ec-3")
		case "flac":
			c = append(c, "fLaC")
		case "opus":
			c = append(c, "Opus")
		case "mp3":
			c = append(c, "mp4a.40.34")
		default:
			c = append(c, "mp4a.40.2")
		}
	}
	return strings.Join(c, ",")
}

func (m *Manager) serveMaster(w http.ResponseWriter, s *Session) {
	d := s.Decision
	bw := s.BitrateKbps() * 1000
	if bw == 0 {
		bw = 8_000_000
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	// Text subtitles as WebVTT renditions, so the player's own menu can switch them.
	subs := s.HLSSubs && len(s.TextSubs) > 0
	if subs {
		for _, t := range s.TextSubs {
			name := t.Title
			if name == "" {
				name = languageLabel(t.Language)
			}
			if t.Forced && !strings.Contains(strings.ToLower(name), "forced") {
				name += " (Forced)"
			}
			def := "NO"
			if t.ID == s.SubtitleStreamID {
				def = "YES"
			}
			fmt.Fprintf(&b, "#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",NAME=\"%s\",DEFAULT=%s,AUTOSELECT=%s,FORCED=%s",
				strings.ReplaceAll(name, `"`, "'"), def, def, map[bool]string{true: "YES", false: "NO"}[t.Forced])
			if t.Language != "" {
				fmt.Fprintf(&b, ",LANGUAGE=\"%s\"", t.Language)
			}
			fmt.Fprintf(&b, ",URI=\"subtitles/%d.m3u8\"\n", t.ID)
		}
	}
	fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"%s\"", bw, codecsString(s))
	if subs {
		b.WriteString(",SUBTITLES=\"subs\"")
	}
	if v := s.Media.Video; v != nil {
		h, wdt := v.Height, v.Width
		if !d.VideoCopy && d.Height > 0 && v.Height > 0 {
			wdt = (v.Width*d.Height/v.Height + 1) &^ 1
			h = d.Height
		}
		fmt.Fprintf(&b, ",RESOLUTION=%dx%d", wdt, h)
		if v.FrameRate > 0 {
			fmt.Fprintf(&b, ",FRAME-RATE=%.3f", v.FrameRate)
		}
		if d.VideoCopy && v.HDR != "" {
			range_ := "PQ"
			if v.HDR == "hlg" {
				range_ = "HLG"
			}
			fmt.Fprintf(&b, ",VIDEO-RANGE=%s", range_)
		}
	}
	b.WriteString("\nindex.m3u8\n")
	// Lower variants for adaptive bitrate (remote transcodes only).
	for i, rg := range s.Rungs() {
		bw := (rg.VideoKbps + audioKbps(d.AudioChannels)) * 1000
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"%s\"", bw, codecsString(s))
		if v := s.Media.Video; v != nil && v.Height > 0 {
			fmt.Fprintf(&b, ",RESOLUTION=%dx%d", (v.Width*rg.Height/v.Height+1)&^1, rg.Height)
		}
		if subs {
			b.WriteString(",SUBTITLES=\"subs\"")
		}
		fmt.Fprintf(&b, "\nr%d/index.m3u8\n", i+1)
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, b.String())
}

func (m *Manager) serveFromTranscoder(w http.ResponseWriter, r *http.Request, s *Session, rung, k int) {
	t, err := m.RungTranscoder(s, rung)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if t == nil {
		http.Error(w, "this session plays the file directly", http.StatusNotFound)
		return
	}
	var p string
	if k < 0 {
		p, err = t.Init(r.Context())
	} else {
		p, err = t.Segment(r.Context(), k)
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("segment failed", "session", s.ID, "segment", k, "err", err)
		}
		http.Error(w, "segment unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, p)
}

// serveSubtitle converts a subtitle track to WebVTT (or extracts the original ASS) once per
// file and caches it.
func (m *Manager) serveSubtitle(w http.ResponseWriter, r *http.Request, s *Session, streamID int64, cacheDir, format string) {
	var index int
	var external string
	err := m.DB.QueryRowContext(r.Context(), `SELECT COALESCE(stream_index, -1), COALESCE(external_path, '') FROM streams
		WHERE id = ? AND file_id = ? AND kind = 'subtitle'`, streamID, s.FileID).Scan(&index, &external)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	out := filepath.Join(cacheDir, fmt.Sprintf("%d-%d.%s", s.FileID, streamID, format))
	if _, err := os.Stat(out); err != nil {
		os.MkdirAll(cacheDir, 0o755)
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		args := []string{"-hide_banner", "-v", "error", "-y"}
		if external != "" {
			args = append(args, "-i", external, "-map", "0:s:0")
		} else {
			args = append(args, "-i", s.Path, "-map", fmt.Sprintf("0:%d", index))
		}
		tmp := out + ".tmp"
		if format == "ass" {
			args = append(args, "-c:s", "ass", "-f", "ass", tmp)
		} else {
			args = append(args, "-c:s", "webvtt", "-f", "webvtt", tmp)
		}
		if msg, err := exec.CommandContext(ctx, m.FFmpeg, args...).CombinedOutput(); err != nil {
			os.Remove(tmp)
			slog.Warn("subtitle conversion failed", "file", s.Path, "stream", streamID, "err", err, "ffmpeg", strings.TrimSpace(string(msg)))
			http.Error(w, "subtitle conversion failed", http.StatusUnprocessableEntity)
			return
		}
		os.Rename(tmp, out)
	}
	if format == "ass" {
		w.Header().Set("Content-Type", "text/x-ssa; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	}
	if r.URL.Query().Get("hls") == "1" && format == "vtt" {
		// HLS WebVTT needs a timestamp map; our media timeline starts at zero.
		data, err := os.ReadFile(out)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		header, rest, _ := bytes.Cut(data, []byte("\n"))
		w.Write(header)
		io.WriteString(w, "\nX-TIMESTAMP-MAP=MPEGTS:0,LOCAL:00:00:00.000\n")
		w.Write(rest)
		return
	}
	http.ServeFile(w, r, out)
}

type fontAttachment struct {
	Index int    `json:"-"`
	Name  string `json:"name"`
	URL   string `json:"url"`
}

// fonts lists font attachments embedded in the file (anime MKVs carry their subtitle fonts).
func (m *Manager) fonts(r *http.Request, s *Session) []fontAttachment {
	var raw string
	if err := m.DB.QueryRowContext(r.Context(), `SELECT COALESCE(probe_json, '') FROM media_files WHERE id = ?`, s.FileID).Scan(&raw); err != nil {
		return nil
	}
	var out struct {
		Streams []struct {
			Index     int               `json:"index"`
			CodecType string            `json:"codec_type"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	var list []fontAttachment
	for _, st := range out.Streams {
		mime := strings.ToLower(st.Tags["mimetype"])
		name := st.Tags["filename"]
		ext := strings.ToLower(filepath.Ext(name))
		if st.CodecType != "attachment" || !(strings.Contains(mime, "font") || ext == ".ttf" || ext == ".otf" || ext == ".ttc" || ext == ".woff" || ext == ".woff2") {
			continue
		}
		list = append(list, fontAttachment{Index: st.Index, Name: name, URL: StreamPrefix + s.ID + "/fonts/" + strconv.Itoa(st.Index)})
	}
	return list
}

func (m *Manager) serveFontList(w http.ResponseWriter, r *http.Request, s *Session) {
	list := m.fonts(r, s)
	if list == nil {
		list = []fontAttachment{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// serveFont extracts one embedded font (cached per file).
func (m *Manager) serveFont(w http.ResponseWriter, r *http.Request, s *Session, index int, cacheDir string) {
	ok := false
	for _, f := range m.fonts(r, s) {
		ok = ok || f.Index == index
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	out := filepath.Join(cacheDir, fmt.Sprintf("%d-font-%d", s.FileID, index))
	if _, err := os.Stat(out); err != nil {
		os.MkdirAll(cacheDir, 0o755)
		tmp := out + ".tmp"
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		// -dump_attachment writes the file and then complains there's no output; the file is what we want.
		exec.CommandContext(ctx, m.FFmpeg, "-hide_banner", "-v", "error", "-y", fmt.Sprintf("-dump_attachment:%d", index), tmp, "-i", s.Path).Run()
		if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
			http.Error(w, "font extraction failed", http.StatusUnprocessableEntity)
			return
		}
		os.Rename(tmp, out)
	}
	w.Header().Set("Content-Type", "font/ttf")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, out)
}

// serveAudioTranscode streams music converted to AAC (progressive; ?start=seconds to seek).
func (m *Manager) serveAudioTranscode(w http.ResponseWriter, r *http.Request, s *Session) {
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	kbps := 256
	if s.LimitKbps > 0 && s.LimitKbps < kbps {
		kbps = max(s.LimitKbps, 64)
	}
	args := []string{"-hide_banner", "-v", "error", "-nostdin"}
	if start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(start, 'f', 3, 64))
	}
	args = append(args, "-i", s.Path, "-map", "0:a:0", "-vn", "-c:a", "aac", "-ac", "2", "-b:a", fmt.Sprintf("%dk", kbps),
		"-f", "adts", "pipe:1")
	cmd := exec.CommandContext(r.Context(), m.FFmpeg, args...)
	cmd.Stdout = w
	w.Header().Set("Content-Type", "audio/aac")
	if err := cmd.Run(); err != nil && r.Context().Err() == nil {
		slog.Warn("audio transcode failed", "file", s.Path, "err", err)
	}
}

// serveSubtitlePlaylist is a one-segment WebVTT media playlist for a subtitle rendition.
func serveSubtitlePlaylist(w http.ResponseWriter, s *Session, streamID int64) {
	found := false
	for _, t := range s.TextSubs {
		found = found || t.ID == streamID
	}
	if !found {
		http.Error(w, "no such subtitle", http.StatusNotFound)
		return
	}
	dur := float64(s.Media.DurationMS) / 1000
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:%.3f,\n%d.vtt?hls=1\n#EXT-X-ENDLIST\n",
		int(math.Ceil(dur)), dur, streamID)
}

// languageLabel names a subtitle track that has no title.
func languageLabel(code string) string {
	names := map[string]string{"eng": "English", "en": "English", "jpn": "Japanese", "ja": "Japanese", "spa": "Spanish", "es": "Spanish",
		"fre": "French", "fra": "French", "fr": "French", "ger": "German", "deu": "German", "de": "German", "ita": "Italian", "it": "Italian",
		"por": "Portuguese", "pt": "Portuguese", "chi": "Chinese", "zho": "Chinese", "zh": "Chinese", "kor": "Korean", "ko": "Korean",
		"rus": "Russian", "ru": "Russian", "ara": "Arabic", "hin": "Hindi", "dut": "Dutch", "nld": "Dutch", "swe": "Swedish", "pol": "Polish"}
	if n, ok := names[strings.ToLower(code)]; ok {
		return n
	}
	if code == "" || code == "und" {
		return "Unknown"
	}
	return strings.ToUpper(code)
}
