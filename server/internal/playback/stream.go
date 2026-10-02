package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
		switch {
		case file == "file":
			m.serveFile(w, r, s)
		case file == "master.m3u8":
			m.serveMaster(w, s)
		case file == "index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(Playlist(s.Media.DurationMS))
		case file == "init.mp4":
			m.serveFromTranscoder(w, r, s, -1)
		case strings.HasSuffix(file, ".m4s"):
			k, err := strconv.Atoi(strings.TrimSuffix(file, ".m4s"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			m.serveFromTranscoder(w, r, s, k)
		case strings.HasPrefix(file, "subtitles/") && strings.HasSuffix(file, ".vtt"):
			sid, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(file, "subtitles/"), ".vtt"), 10, 64)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			m.serveSubtitle(w, r, s, sid, subtitleCache)
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
	fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"%s\"", bw, codecsString(s))
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
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, b.String())
}

func (m *Manager) serveFromTranscoder(w http.ResponseWriter, r *http.Request, s *Session, k int) {
	t := s.Transcoder()
	if t == nil {
		http.Error(w, "this session plays the file directly", http.StatusNotFound)
		return
	}
	var p string
	var err error
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

// serveSubtitle converts a subtitle track to WebVTT once per file and caches it.
func (m *Manager) serveSubtitle(w http.ResponseWriter, r *http.Request, s *Session, streamID int64, cacheDir string) {
	var index int
	var external string
	err := m.DB.QueryRowContext(r.Context(), `SELECT COALESCE(stream_index, -1), COALESCE(external_path, '') FROM streams
		WHERE id = ? AND file_id = ? AND kind = 'subtitle'`, streamID, s.FileID).Scan(&index, &external)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	out := filepath.Join(cacheDir, fmt.Sprintf("%d-%d.vtt", s.FileID, streamID))
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
		args = append(args, "-c:s", "webvtt", "-f", "webvtt", tmp)
		if msg, err := exec.CommandContext(ctx, m.FFmpeg, args...).CombinedOutput(); err != nil {
			os.Remove(tmp)
			slog.Warn("subtitle conversion failed", "file", s.Path, "stream", streamID, "err", err, "ffmpeg", strings.TrimSpace(string(msg)))
			http.Error(w, "subtitle conversion failed", http.StatusUnprocessableEntity)
			return
		}
		os.Rename(tmp, out)
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
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
