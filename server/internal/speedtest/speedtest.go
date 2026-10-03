// Package speedtest measures the server's internet speed against Cloudflare's public speed
// test (speed.cloudflare.com), so admins can set Remote access's upload speed from a real
// figure. It only runs when an admin asks; nothing else is sent.
package speedtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type Result struct {
	DownloadMbps, UploadMbps, LatencyMs float64
	TestedAt                            time.Time
}

type Tester struct {
	BaseURL string // default https://speed.cloudflare.com
	HTTP    *http.Client
	// Each direction runs for about this long.
	Duration time.Duration
}

func (t *Tester) base() string {
	if t.BaseURL != "" {
		return t.BaseURL
	}
	return "https://speed.cloudflare.com"
}

func (t *Tester) client() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (t *Tester) duration() time.Duration {
	if t.Duration > 0 {
		return t.Duration
	}
	return 8 * time.Second
}

// Run measures latency, then download, then upload.
func (t *Tester) Run(ctx context.Context) (Result, error) {
	r := Result{TestedAt: time.Now().UTC()}
	var err error
	if r.LatencyMs, err = t.latency(ctx); err != nil {
		return r, err
	}
	if r.DownloadMbps, err = t.download(ctx); err != nil {
		return r, err
	}
	r.UploadMbps, err = t.upload(ctx)
	return r, err
}

func (t *Tester) latency(ctx context.Context) (float64, error) {
	var samples []float64
	for range 5 {
		start := time.Now()
		if err := t.get(ctx, 0, io.Discard); err != nil {
			return 0, err
		}
		samples = append(samples, float64(time.Since(start).Microseconds())/1000)
	}
	sort.Float64s(samples)
	return samples[len(samples)/2], nil
}

// download fetches 25 MB chunks until the time is up, counting bytes as they arrive.
func (t *Tester) download(ctx context.Context) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, t.duration())
	defer cancel()
	start := time.Now()
	var n countWriter
	for ctx.Err() == nil {
		if err := t.get(ctx, 25<<20, &n); err != nil && ctx.Err() == nil {
			return 0, err
		}
	}
	return mbps(int64(n), time.Since(start)), nil
}

// upload posts 10 MB bodies until the time is up.
func (t *Tester) upload(ctx context.Context) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, t.duration())
	defer cancel()
	body := make([]byte, 10<<20)
	start := time.Now()
	var sent int64
	for ctx.Err() == nil {
		rd := &countReader{r: bytes.NewReader(body)}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base()+"/__up", rd)
		if err != nil {
			return 0, err
		}
		req.ContentLength = int64(len(body))
		resp, err := t.client().Do(req)
		sent += rd.n
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return 0, err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return 0, fmt.Errorf("speed test upload: HTTP %d", resp.StatusCode)
		}
	}
	return mbps(sent, time.Since(start)), nil
}

func (t *Tester) get(ctx context.Context, size int, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/__down?bytes=%d", t.base(), size), nil)
	if err != nil {
		return err
	}
	resp, err := t.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("speed test: HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func mbps(n int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) * 8 / d.Seconds() / 1e6
}

type countWriter int64

func (c *countWriter) Write(p []byte) (int, error) { *c += countWriter(len(p)); return len(p), nil }

type countReader struct {
	r *bytes.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
