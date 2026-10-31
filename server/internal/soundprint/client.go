// Package soundprint is Marquee's music intelligence (M6.5): it has the Soundprint analysis sidecar
// embed every track, keeps the embeddings in memory, and builds similar tracks, radios,
// Sound Journey, Muse playlists and mixes from them (D56).
package soundprint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to the analysis sidecar (soundprint/app.py).
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
	Model  string `json:"model"` // the general sound model's analysis id
	Device string `json:"device"`
	Dims   int    `json:"dims"`
	// Models are the sound models the sidecar offers (by name: general, music) and their
	// analysis ids; Traits is the traits analysis id. Empty from an older sidecar.
	Models map[string]string `json:"models"`
	Traits string            `json:"traits"`
}

// ModelID is the analysis id of a sound model by name ("" when the sidecar doesn't offer it).
func (h Health) ModelID(name string) string {
	if id, ok := h.Models[name]; ok {
		return id
	}
	if name == ModelGeneral {
		return h.Model
	}
	return ""
}

// Traits is what a track is like, 0–1 each (relative: compared across the library).
type Traits struct {
	Happy      float64 `json:"happy"`
	Sad        float64 `json:"sad"`
	Tender     float64 `json:"tender"`
	Exciting   float64 `json:"exciting"`
	Angry      float64 `json:"angry"`
	Scary      float64 `json:"scary"`
	Dance      float64 `json:"dance"`
	Vocal      float64 `json:"vocal"`
	Electronic float64 `json:"electronic"`
	Acoustic   float64 `json:"acoustic"`
	Ambient    float64 `json:"ambient"`
}

type TraitsResult struct {
	Path   string `json:"path"`
	Traits Traits `json:"traits"`
	Error  string `json:"error"`
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
		return fmt.Errorf("soundprint %s: HTTP %d", path, resp.StatusCode)
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
		return h, fmt.Errorf("soundprint health: HTTP %d", resp.StatusCode)
	}
	return h, json.NewDecoder(resp.Body).Decode(&h)
}

// Analyze embeds and measures up to 64 files with a sound model (ModelGeneral or
// ModelMusic); without features only the embeddings are made.
func (c *Client) Analyze(ctx context.Context, paths []string, model string, features bool) (string, []Analysis, error) {
	var out struct {
		Model   string     `json:"model"`
		Results []Analysis `json:"results"`
	}
	err := c.post(ctx, "/analyze", map[string]any{"paths": paths, "model": model, "features": features}, &out)
	return out.Model, out.Results, err
}

// Traits measures what up to 64 files are like.
func (c *Client) Traits(ctx context.Context, paths []string) (string, []TraitsResult, error) {
	var out struct {
		Model   string         `json:"model"`
		Results []TraitsResult `json:"results"`
	}
	err := c.post(ctx, "/traits", map[string]any{"paths": paths}, &out)
	return out.Model, out.Results, err
}

// EmbedText embeds prompts into the same space as a sound model's tracks.
func (c *Client) EmbedText(ctx context.Context, texts []string, model string) ([][]float32, error) {
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := c.post(ctx, "/embed_text", map[string]any{"texts": texts, "model": model}, &out)
	return out.Embeddings, err
}

// EmbedDocs embeds movie and show descriptions (kind "doc") or Muse prompts (kind "query")
// with the sidecar's text model (USER-15, USER-16). It returns the model id, which an
// empty texts list also reports without loading the model.
func (c *Client) EmbedDocs(ctx context.Context, texts []string, kind string) (string, [][]float32, error) {
	var out struct {
		Model      string      `json:"model"`
		Embeddings [][]float32 `json:"embeddings"`
	}
	if texts == nil {
		texts = []string{}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute) // the first call downloads and loads the model
	defer cancel()
	err := c.post(ctx, "/embed_docs", map[string]any{"texts": texts, "kind": kind}, &out)
	if err == nil && len(out.Embeddings) != len(texts) {
		err = fmt.Errorf("soundprint /embed_docs: %d embeddings for %d texts", len(out.Embeddings), len(texts))
	}
	return out.Model, out.Embeddings, err
}
