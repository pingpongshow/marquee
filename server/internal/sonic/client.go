// Package sonic is Marquee's music intelligence (M6.5): it has the sonic analysis sidecar
// embed every track, keeps the embeddings in memory, and builds similar tracks, radios,
// Sonic Adventure, Sonic Sage playlists and mixes from them (D56).
package sonic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to the analysis sidecar (sonic/app.py).
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

type Analysis struct {
	Path      string    `json:"path"`
	Duration  float64   `json:"duration"`
	BPM       *float64  `json:"bpm"`
	Key       *string   `json:"key"`
	Mode      *string   `json:"mode"`
	Energy    *float64  `json:"energy"`
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error"`
}

type Health struct {
	Model  string `json:"model"`
	Device string `json:"device"`
	Dims   int    `json:"dims"`
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sonic %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Health reports the model in use, or an error when the sidecar isn't running.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return h, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return h, fmt.Errorf("sonic health: HTTP %d", resp.StatusCode)
	}
	return h, json.NewDecoder(resp.Body).Decode(&h)
}

// Analyze embeds and measures up to 64 files.
func (c *Client) Analyze(ctx context.Context, paths []string) (string, []Analysis, error) {
	var out struct {
		Model   string     `json:"model"`
		Results []Analysis `json:"results"`
	}
	err := c.post(ctx, "/analyze", map[string]any{"paths": paths}, &out)
	return out.Model, out.Results, err
}

// EmbedText embeds prompts into the same space as tracks.
func (c *Client) EmbedText(ctx context.Context, texts []string) ([][]float32, error) {
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := c.post(ctx, "/embed_text", map[string]any{"texts": texts}, &out)
	return out.Embeddings, err
}
