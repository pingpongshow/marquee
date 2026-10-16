package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Remote media files (movies on demand from an Xtream Codes server): read over HTTP with
// Range requests, so a film's keyframe index (Matroska Cues, MP4 sample tables) is found
// without downloading the film.

const remoteBlock = 1 << 20

// remoteFile reads an HTTP resource in 1 MB blocks by Range requests (io.ReaderAt and
// io.ReadSeeker). It keeps the blocks it has read; indexes are a few MB at most.
type remoteFile struct {
	ctx    context.Context
	client *http.Client
	url    string
	ua     string
	size   int64

	mu     sync.Mutex
	blocks map[int64][]byte
	pos    int64
	reads  int
}

// maxRemoteReads bounds the requests one index read may make.
const maxRemoteReads = 64

var errNoRanges = errors.New("the server doesn't support Range requests")

func openRemote(ctx context.Context, client *http.Client, url, ua string) (*remoteFile, error) {
	f := &remoteFile{ctx: ctx, client: client, url: url, ua: ua, blocks: map[int64][]byte{}, size: -1}
	if _, err := f.block(0); err != nil {
		return nil, err
	}
	if f.size < 0 {
		return nil, errNoRanges
	}
	return f, nil
}

// block returns block n, fetching it if needed. Caller must not hold f.mu.
func (f *remoteFile) block(n int64) ([]byte, error) {
	f.mu.Lock()
	b, ok := f.blocks[n]
	if ok {
		f.mu.Unlock()
		return b, nil
	}
	if f.reads >= maxRemoteReads {
		f.mu.Unlock()
		return nil, errors.New("too many reads for a keyframe index")
	}
	f.reads++
	f.mu.Unlock()
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, errors.New("bad link")
	}
	if f.ua != "" {
		req.Header.Set("User-Agent", f.ua)
	}
	start := n * remoteBlock
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+remoteBlock-1))
	resp, err := f.client.Do(req)
	if err != nil {
		var ue interface{ Unwrap() error }
		if errors.As(err, &ue) && ue.Unwrap() != nil {
			err = ue.Unwrap() // *url.Error names the link, which may hold a password
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return nil, io.EOF
	}
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("%w (answered %d)", errNoRanges, resp.StatusCode)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndexByte(cr, '/'); i >= 0 {
			if total, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
				f.mu.Lock()
				f.size = total
				f.mu.Unlock()
			}
		}
	}
	b, err = io.ReadAll(io.LimitReader(resp.Body, remoteBlock))
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.blocks[n] = b
	f.mu.Unlock()
	return b, nil
}

func (f *remoteFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	n := 0
	for n < len(p) {
		if f.size >= 0 && off+int64(n) >= f.size {
			return n, io.EOF
		}
		at := off + int64(n)
		b, err := f.block(at / remoteBlock)
		if err != nil {
			return n, err
		}
		i := int(at % remoteBlock)
		if i >= len(b) {
			return n, io.EOF
		}
		n += copy(p[n:], b[i:])
	}
	return n, nil
}

func (f *remoteFile) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.pos)
	f.pos += int64(n)
	if n > 0 && err == io.EOF {
		err = nil
	}
	return n, err
}

func (f *remoteFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += f.size
	}
	if offset < 0 {
		return 0, errors.New("negative position")
	}
	f.pos = offset
	return offset, nil
}

// RemoteKeyframes reads a remote file's keyframe times from its index (Matroska Cues or
// MP4 sample tables) by Range requests. Files without one return an error: their video is
// transcoded rather than scanned over the network.
func RemoteKeyframes(ctx context.Context, client *http.Client, url, userAgent, container string) ([]float64, error) {
	f, err := openRemote(ctx, client, url, userAgent)
	if err != nil {
		return nil, err
	}
	var times []float64
	switch normalizeContainer(container) {
	case "mkv", "webm":
		times, err = matroskaKeyframesFrom(f)
	case "mp4":
		times, err = mp4KeyframesFrom(f)
	default:
		err = errNoIndex
	}
	if err != nil {
		return nil, err
	}
	if len(times) < 2 {
		return nil, errNoIndex
	}
	sort.Float64s(times)
	return times, nil
}
