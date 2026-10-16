package vodsave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// spaceMargin is kept free on the volume besides what a save needs.
const spaceMargin = 256 << 20

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	// No overall timeout (a film takes a while); stalls are caught by the body read deadline.
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout: 90 * time.Second}}
}

// run saves one film or episode: download, convert when asked, verify, move into place.
func (s *Service) run(ctx context.Context, j *job, v Save) (outcome, error) {
	c := s.cfg()
	prov := v.TargetPath
	if c.SkipExisting && v.ReplaceFileID == 0 {
		season, episode := episodeOf(prov)
		if f := existingFile(prov, v.Kind, season, episode); f != "" {
			return outcome{}, skipError{"Already saved: " + filepath.Base(f)}
		}
	}
	if err := mkdirs(filepath.Dir(prov)); err != nil {
		return outcome{}, permError{fmt.Errorf("couldn't make the folder: %w", err), true}
	}
	srcExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(v.File), "."))
	mode := v.Quality
	if mode == "original" && !keptContainers[srcExt] {
		mode = "remux" // only the container is changed
	}
	processed := mode != "original"
	dl := prov + ".part"
	if processed {
		dl = prov + ".source.part"
	}
	link, ua, err := s.VOD.FileLink(v.SourceID, v.Kind == "episode", v.File)
	if err != nil {
		return outcome{}, permError{err, false}
	}
	sum, total, err := s.download(ctx, j, v, link, ua, dl, processed)
	if err != nil {
		return outcome{}, err
	}
	st, err := os.Stat(dl)
	if err != nil {
		return outcome{}, err
	}
	if total > 0 && st.Size() != total {
		return outcome{}, verifyError{fmt.Errorf("the file is %d bytes; the source said %d", st.Size(), total)}
	}
	j.setStep(Verifying)
	in, err := s.probe(ctx, dl)
	if err != nil {
		return outcome{}, verifyError{err}
	}
	out, info, expected := dl, in, v.ExpectedMS
	strict := false
	if processed {
		j.setStep(Processing)
		s.setState(ctx, v.ID, Processing)
		out = prov + ".part"
		if err := s.process(ctx, j, c, mode, dl, out, in); err != nil {
			os.Remove(out)
			if ctx.Err() != nil {
				return outcome{}, ctx.Err()
			}
			return outcome{}, permError{err, false}
		}
		if info, err = s.probe(ctx, out); err != nil {
			return outcome{}, verifyError{fmt.Errorf("the converted file: %w", err)}
		}
		expected, strict = in.DurationMS, true // checked against the downloaded file
	}
	j.setStep(Verifying)
	s.setState(ctx, v.ID, Verifying)
	note, err := s.verify(ctx, out, info, expected, strict)
	if err != nil {
		if ctx.Err() != nil {
			return outcome{}, ctx.Err()
		}
		return outcome{}, verifyError{err}
	}
	if processed {
		if sum, err = fileSHA256(ctx, out); err != nil {
			return outcome{}, err
		}
	}
	var final, replaced string
	if v.ReplaceFileID > 0 {
		final, replaced, err = s.replace(ctx, v, out, withTag(prov, info.Width, info.Height))
	} else {
		final, err = place(out, withTag(prov, info.Width, info.Height))
	}
	if err != nil {
		var pe permError
		if errors.As(err, &pe) {
			return outcome{}, err
		}
		return outcome{}, permError{fmt.Errorf("couldn't move the file into the library: %w", err), true}
	}
	if processed {
		os.Remove(dl)
	}
	fst, _ := os.Stat(final)
	var size int64
	if fst != nil {
		size = fst.Size()
	}
	return outcome{path: final, note: note, sha: sum, bytes: size, replaced: replaced}, nil
}

// withTag adds the quality tag ("WEBDL-1080p") to a file name.
func withTag(path string, w, h int) string {
	r := Resolution(w, h)
	if r == 0 {
		return path
	}
	ext := filepath.Ext(path)
	return fmt.Sprintf("%s WEBDL-%dp%s", strings.TrimSuffix(path, ext), r, ext)
}

// place moves a finished file to its name without ever replacing a file: it is linked
// there (which fails if something is), else renamed after a check.
func place(from, to string) (string, error) {
	for range 100 {
		to = freeName(to)
		err := os.Link(from, to)
		if err == nil {
			os.Remove(from)
			os.Chmod(to, 0o664)
			return to, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue // appeared just now: the next free name
		}
		// No hard links here (some file systems): rename, having just checked the name is free.
		if err := os.Rename(from, to); err != nil {
			return "", err
		}
		os.Chmod(to, 0o664)
		return to, nil
	}
	return "", errors.New("no free file name")
}

var contentRange = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+|\*)$`)

// download fetches the source's file into part, continuing a partial one with a Range
// request (a server that ignores it sends the whole file, and the download starts over). It
// returns the file's SHA-256 and the size the source gave (0 = unknown).
func (s *Service) download(ctx context.Context, j *job, v Save, link, ua, part string, processed bool) (string, int64, error) {
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o664)
	if err != nil {
		return "", 0, permError{err, true}
	}
	defer f.Close()
	os.Chmod(part, 0o664)
	st, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	have := st.Size()
	h := sha256.New()
	// Already complete (stopped while converting or verifying).
	if have > 0 && v.BytesTotal > 0 && have == v.BytesTotal {
		if _, err := io.Copy(h, f); err != nil {
			return "", 0, err
		}
		j.setBytes(have, have)
		return hex.EncodeToString(h.Sum(nil)), have, nil
	}
	for attempt := 0; ; attempt++ {
		resp, err := s.get(ctx, link, ua, have)
		if err != nil {
			return "", 0, err
		}
		total := int64(0)
		switch {
		case resp.StatusCode == http.StatusPartialContent && have > 0:
			m := contentRange.FindStringSubmatch(resp.Header.Get("Content-Range"))
			start := int64(-1)
			if m != nil {
				start, _ = strconv.ParseInt(m[1], 10, 64)
				total, _ = strconv.ParseInt(m[3], 10, 64)
			}
			if start != have {
				resp.Body.Close()
				have = 0 // an answer for another range: start over
				if err := restart(f, h); err != nil {
					return "", 0, err
				}
				continue
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				resp.Body.Close()
				return "", 0, err
			}
			if _, err := io.CopyN(h, f, have); err != nil { // the checksum covers what was there
				resp.Body.Close()
				return "", 0, err
			}
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent:
			// The whole file (the server ignored Range, or there was nothing yet).
			if have > 0 {
				if err := restart(f, h); err != nil {
					resp.Body.Close()
					return "", 0, err
				}
				have = 0
			}
			if resp.ContentLength > 0 {
				total = resp.ContentLength
			}
		case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && have > 0 && attempt == 0:
			resp.Body.Close()
			have = 0 // the partial file doesn't fit the source's any more
			if err := restart(f, h); err != nil {
				return "", 0, err
			}
			continue
		case resp.StatusCode >= 500:
			resp.Body.Close()
			return "", 0, netError{fmt.Errorf("the source answered %s", resp.Status)}
		default:
			resp.Body.Close()
			return "", 0, permError{fmt.Errorf("the source answered %s", resp.Status), resp.StatusCode == http.StatusTooManyRequests}
		}
		s.sourceUp(v.SourceID)
		err = s.receive(ctx, j, v, resp, f, h, have, total, processed)
		resp.Body.Close()
		if err != nil {
			return "", 0, err
		}
		return hex.EncodeToString(h.Sum(nil)), total, nil
	}
}

func restart(f *os.File, h hash.Hash) error {
	h.Reset()
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.Seek(0, io.SeekStart)
	return err
}

func (s *Service) get(ctx context.Context, link, ua string, from int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, permError{errors.New("the source's link isn't valid"), false}
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if from > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
	}
	resp, err := s.client().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, netError{err}
	}
	return resp, nil
}

// receive writes the body after what the file has, checking there is room first.
func (s *Service) receive(ctx context.Context, j *job, v Save, resp *http.Response, f *os.File, h hash.Hash,
	have, total int64, processed bool) error {
	if total > 0 {
		need := total - have + spaceMargin
		if processed {
			need += total // the converted copy, about as big
		}
		free, err := s.freeSpace(filepath.Dir(f.Name()))
		if err == nil && free < uint64(need) {
			return permError{fmt.Errorf("not enough disk space: %s needed, %s free", sizeText(need), sizeText(int64(free))), true}
		}
	}
	s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_total=?, bytes_done=? WHERE id=?", total, have, v.ID)
	j.setBytes(have, total)
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, 1<<20)
	done := have
	lastFlush := time.Now()
	// A stalled connection (no bytes for a minute) counts as a network error.
	stall := time.AfterFunc(time.Minute, func() { resp.Body.Close() })
	defer stall.Stop()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(time.Minute)
			if _, err := f.Write(buf[:n]); err != nil {
				return permError{fmt.Errorf("couldn't write the file: %w", err), true}
			}
			h.Write(buf[:n])
			done += int64(n)
			j.add(int64(n))
			if time.Since(lastFlush) >= 2*time.Second {
				lastFlush = time.Now()
				s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_done=? WHERE id=?", done, v.ID)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			s.DB.ExecContext(context.WithoutCancel(ctx), "UPDATE vod_saves SET bytes_done=? WHERE id=?", done, v.ID)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return netError{rerr}
		}
	}
	if total > 0 && done < total {
		return netError{fmt.Errorf("the connection ended after %s of %s", sizeText(done), sizeText(total))}
	}
	s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_done=? WHERE id=?", done, v.ID)
	return f.Sync()
}

func (s *Service) freeSpace(dir string) (uint64, error) {
	if s.FreeSpace != nil {
		return s.FreeSpace(dir)
	}
	return freeBytes(dir)
}

func fileSHA256(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), ctx.Err()
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n>>10)
}
