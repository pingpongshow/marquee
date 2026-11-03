package speakers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The routes speakers fetch from. A speaker can't send a sign-in token, so they're outside
// sign-in: a URL is good only for the songs of the live session whose random token it carries,
// and only while that session lasts.
const (
	StreamRoute  = "GET " + streamPrefix + "{token}/{file}"
	ArtRoute     = "GET " + artPrefix + "{token}/{file}"
	streamPrefix = "/api/v1/speakers/stream/"
	artPrefix    = "/api/v1/speakers/art/"
	// artWidth is plenty for what the Sonos app and speakers show.
	artWidth = 480
	// Speakers play nothing above these.
	maxPlayableRate = 48000
	maxPlayableBits = 24
)

// prepared is a song ready for its speaker to fetch, or on its way there: done closes when
// a conversion ends, and the fields are read only after that.
type prepared struct {
	done      chan struct{}
	err       error
	path      string
	ext, mime string
	converted bool
	cancel    context.CancelFunc
}

// sourceFile is a song's file and audio stream.
type sourceFile struct {
	ID                   int64
	Path, Codec          string
	Rate, Bits, Channels int
}

// source finds a song's file, chosen as playback does (the first available one).
func (s *Service) source(ctx context.Context, itemID int64) (sourceFile, error) {
	var f sourceFile
	err := s.DB.QueryRowContext(ctx, `SELECT f.id, f.path, st.codec, COALESCE(st.sample_rate, 0), COALESCE(st.bit_depth, 0), COALESCE(st.channels, 0)
		FROM media_files f JOIN media_versions v ON v.id = f.version_id JOIN streams st ON st.file_id = f.id AND st.kind = 'audio'
		WHERE v.item_id = ? AND f.available = 1 ORDER BY COALESCE(f.height, 0) DESC, f.part_index, st.id LIMIT 1`, itemID).
		Scan(&f.ID, &f.Path, &f.Codec, &f.Rate, &f.Bits, &f.Channels)
	if errors.Is(err, sql.ErrNoRows) {
		return f, errors.New("no available file")
	}
	return f, err
}

// playable decides whether a speaker can play a file as it is, and what to call it: MP3, AAC,
// FLAC and ALAC, stereo at most, up to 48 kHz and 24 bits, in their usual files. An unknown
// rate is taken as fine only for the lossy formats, which are never above 48 kHz in practice.
func playable(f sourceFile) (ext, mime string, ok bool) {
	if f.Rate > maxPlayableRate || f.Bits > maxPlayableBits || f.Channels > 2 {
		return "", "", false
	}
	fileExt := strings.ToLower(filepath.Ext(f.Path))
	switch f.Codec {
	case "mp3":
		if fileExt == ".mp3" {
			return ".mp3", "audio/mpeg", true
		}
	case "aac":
		if fileExt == ".m4a" || fileExt == ".mp4" {
			return ".m4a", "audio/mp4", true
		}
		if fileExt == ".aac" {
			return ".aac", "audio/aac", true
		}
	case "flac":
		if fileExt == ".flac" && f.Rate > 0 {
			return ".flac", "audio/flac", true
		}
	case "alac":
		if (fileExt == ".m4a" || fileExt == ".mp4") && f.Rate > 0 {
			return ".m4a", "audio/mp4", true
		}
	}
	return "", "", false
}

// conversionRate is the sample rate to convert a file to, 0 to keep it: the same family as the
// original where it's above what a speaker plays. Surround AAC small enough for ConvertAudio to
// copy (as it should for a phone) names its own rate, which has it encoded, and so downmixed.
func conversionRate(f sourceFile) int {
	switch {
	case f.Rate == 0:
		return maxPlayableRate
	case f.Rate <= maxPlayableRate:
		if f.Channels > 2 {
			return f.Rate
		}
		return 0
	case f.Rate%44100 == 0:
		return 44100
	}
	return maxPlayableRate
}

// prepare gets a song ready, once: at once for a file the speaker can play, in the background
// for one to convert. mu is held.
func (ss *session) prepare(t Track) *prepared {
	ss.dataMu.Lock()
	p := ss.files[t.ItemID]
	if p != nil {
		ss.dataMu.Unlock()
		return p
	}
	p = &prepared{done: make(chan struct{})}
	ss.files[t.ItemID] = p
	ss.dataMu.Unlock()
	f, err := ss.svc.source(ss.ctx, t.ItemID)
	if err != nil {
		p.err = err
		close(p.done)
		return p
	}
	if ext, mime, ok := playable(f); ok {
		p.path, p.ext, p.mime = f.Path, ext, mime
		close(p.done)
		return p
	}
	p.converted, p.ext, p.mime = true, ".m4a", "audio/mp4"
	p.path = filepath.Join(ss.dir, strconv.FormatInt(t.ItemID, 10)+p.ext)
	ctx, cancel := context.WithCancel(ss.ctx)
	p.cancel = cancel
	go func() {
		defer cancel()
		p.err = ss.convert(ctx, t.ItemID, f, p.path)
		close(p.done)
		select {
		case ss.wake <- struct{}{}:
		default:
		}
	}()
	return p
}

// convert writes beside the final name, so a finished name is always a finished file.
func (ss *session) convert(ctx context.Context, itemID int64, f sourceFile, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".part"
	err := ss.svc.Convert(ctx, ss.owner, itemID, f.ID, conversionRate(f), tmp)
	if err == nil {
		err = os.Rename(tmp, out)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// evict throws away conversions not needed for the song playing or the next: a long hi-res
// queue shouldn't fill the disk. Going back to one converts it again.
func (ss *session) evict() {
	ss.dataMu.Lock()
	defer ss.dataMu.Unlock()
	keep := map[int64]bool{}
	for i := ss.index; i <= ss.index+1 && i < len(ss.queue); i++ {
		keep[ss.queue[i].ItemID] = true
	}
	for id, p := range ss.files {
		if p.converted && !keep[id] {
			p.cancel()
			os.Remove(p.path)
			delete(ss.files, id)
		}
	}
}

// served returns a song of the session that's ready, for its URL's extension.
func (ss *session) served(itemID int64, ext string) (*prepared, bool) {
	ss.dataMu.Lock()
	defer ss.dataMu.Unlock()
	if ss.ended || !ss.inQueue(itemID) {
		return nil, false
	}
	p := ss.files[itemID]
	if p == nil {
		return nil, false
	}
	select {
	case <-p.done:
		return p, p.err == nil && p.ext == ext
	default:
		return nil, false
	}
}

func (ss *session) inQueue(itemID int64) bool {
	for _, t := range ss.queue {
		if t.ItemID == itemID {
			return true
		}
	}
	return false
}

// artworkOf returns the artwork of a song in the queue: a session can reach no other.
func (ss *session) artworkOf(itemID int64) int64 {
	ss.dataMu.Lock()
	defer ss.dataMu.Unlock()
	if ss.ended {
		return 0
	}
	for _, t := range ss.queue {
		if t.ItemID == itemID {
			return t.ArtworkID
		}
	}
	return 0
}

// Handler serves StreamRoute and ArtRoute. Range requests go through http.ServeContent, as a
// speaker seeks by them.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.init()
		s.mu.Lock()
		ss := s.tokens[r.PathValue("token")]
		s.mu.Unlock()
		name := r.PathValue("file")
		ext := filepath.Ext(name)
		itemID, err := strconv.ParseInt(strings.TrimSuffix(name, ext), 10, 64)
		if ss == nil || err != nil {
			http.NotFound(w, r)
			return
		}
		var path, ctype string
		if strings.HasPrefix(r.URL.Path, artPrefix) {
			art := ss.artworkOf(itemID)
			if art == 0 || s.Art == nil {
				http.NotFound(w, r)
				return
			}
			path, ctype, err = s.Art(r.Context(), art, artWidth)
			if err != nil {
				slog.DebugContext(r.Context(), "speakers: artwork", "item", itemID, "err", err)
				http.NotFound(w, r)
				return
			}
		} else {
			p, ok := ss.served(itemID, ext)
			if !ok {
				http.NotFound(w, r)
				return
			}
			path, ctype = p.path, p.mime
		}
		f, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		http.ServeContent(w, r, "", st.ModTime(), f)
	})
}
