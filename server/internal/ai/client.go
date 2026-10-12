// Package ai talks to a local Ollama server, the optional language model behind smarter
// Muse prompts, re-ranking, reasons and lyric themes. Everything that uses it must work
// without it: calls fail fast (ErrDisabled, ErrUnavailable) and callers fall back.
//
// Every request sends keep_alive, so Ollama unloads the models after the configured idle
// time and the GPU is free when nothing is being asked.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	// ErrDisabled means AI is turned off in settings.
	ErrDisabled = errors.New("AI is turned off")
	// ErrUnavailable means Ollama didn't answer (or recently failed and is paused).
	ErrUnavailable = errors.New("the AI service isn't available")
)

// Config is the part of the settings the client needs.
type Config struct {
	Enabled       bool
	URL           string
	ChatModel     string
	EmbedModel    string
	UnloadMinutes int
}

// Client calls Ollama. Config is read on every call, so settings changes apply at once.
type Client struct {
	Config func() Config
	HTTP   *http.Client
	// ChatTimeout bounds one chat call (default 25 s; a cold model load takes a few seconds).
	ChatTimeout time.Duration
	// EmbedTimeout bounds one embedding call (default 60 s).
	EmbedTimeout time.Duration
	// Now is the clock (tests).
	Now func() time.Time

	mu        sync.Mutex
	failures  int       // consecutive failures
	openUntil time.Time // no calls before this
	lastErr   string
	lastErrAt time.Time
}

// breaker settings: after breakerAfter consecutive failures, calls fail at once for a
// cool-off that doubles with each further failure, up to breakerMax.
const (
	breakerAfter = 2
	breakerBase  = 30 * time.Second
	breakerMax   = 5 * time.Minute
)

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) cfg() Config {
	if c == nil || c.Config == nil {
		return Config{}
	}
	return c.Config()
}

// Enabled reports whether AI is turned on (it may still be unreachable).
func (c *Client) Enabled() bool { return c.cfg().Enabled }

// Ready reports whether a call may be tried now: turned on and not paused after failures.
func (c *Client) Ready() bool {
	if !c.Enabled() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.now().Before(c.openUntil)
}

// EmbedModel is the configured embedding model.
func (c *Client) EmbedModel() string { return c.cfg().EmbedModel }

// keepAlive is the keep_alive value for every request.
func keepAlive(cfg Config) string { return fmt.Sprintf("%dm", max(cfg.UnloadMinutes, 0)) }

// begin checks the switch and the breaker.
func (c *Client) begin() (Config, error) {
	cfg := c.cfg()
	if !cfg.Enabled {
		return cfg, ErrDisabled
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Before(c.openUntil) {
		return cfg, fmt.Errorf("%w (paused after errors: %s)", ErrUnavailable, c.lastErr)
	}
	return cfg, nil
}

// done records a call's outcome. A caller giving up (its context ended) isn't Ollama's fault.
func (c *Client) done(parent context.Context, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.failures, c.openUntil = 0, time.Time{}
		return
	}
	if parent.Err() != nil {
		return
	}
	c.failures++
	c.lastErr, c.lastErrAt = err.Error(), c.now()
	if c.failures >= breakerAfter {
		d := breakerBase << min(c.failures-breakerAfter, 4)
		c.openUntil = c.now().Add(min(d, breakerMax))
	}
}

// LastError is the most recent failure (empty when none).
func (c *Client) LastError() (string, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr, c.lastErrAt, c.now().Before(c.openUntil)
}

func (c *Client) post(ctx context.Context, base, path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) get(ctx context.Context, base, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(msg, &e) == nil && e.Error != "" {
			return fmt.Errorf("ollama %s: %s", req.URL.Path, e.Error)
		}
		return fmt.Errorf("ollama %s: HTTP %d", req.URL.Path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
}

// Chat asks the chat model for a JSON answer matching schema (a JSON Schema object) and
// decodes it into out. Thinking is off and the temperature low, for fast, steady answers.
func (c *Client) Chat(ctx context.Context, system, user string, schema any, out any) error {
	cfg, err := c.begin()
	if err != nil {
		return err
	}
	t := c.ChatTimeout
	if t == 0 {
		t = 25 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	in := map[string]any{
		"model":      cfg.ChatModel,
		"messages":   []Message{{"system", system}, {"user", user}},
		"stream":     false,
		"format":     schema,
		"think":      false,
		"keep_alive": keepAlive(cfg),
		"options":    map[string]any{"temperature": 0.1},
	}
	var resp struct {
		Message Message `json:"message"`
	}
	err = c.post(cctx, cfg.URL, "/api/chat", in, &resp)
	c.done(ctx, err)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	content := strings.TrimSpace(resp.Message.Content)
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return fmt.Errorf("ai: the answer isn't the JSON asked for: %w", err)
	}
	return nil
}

// Embed embeds texts with the embedding model, in one request.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	cfg, err := c.begin()
	if err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, nil
	}
	t := c.EmbedTimeout
	if t == 0 {
		t = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	var resp struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	err = c.post(cctx, cfg.URL, "/api/embed", map[string]any{
		"model": cfg.EmbedModel, "input": texts, "truncate": true, "keep_alive": keepAlive(cfg),
	}, &resp)
	if err == nil && len(resp.Embeddings) != len(texts) {
		err = fmt.Errorf("ollama /api/embed: %d embeddings for %d texts", len(resp.Embeddings), len(texts))
	}
	c.done(ctx, err)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return resp.Embeddings, nil
}

// LoadedModel is a model Ollama holds in memory.
type LoadedModel struct {
	Name      string
	VRAMBytes int64
	ExpiresAt time.Time
}

// Status is what the admin sees.
type Status struct {
	Reachable                     bool
	Version                       string
	Models                        []string
	ChatInstalled, EmbedInstalled bool
	Loaded                        []LoadedModel
	Error                         string
}

// Check asks Ollama at cfg.URL for its version, installed models and loaded models. It
// loads no model and doesn't need AI to be turned on (the admin tests before saving).
func (c *Client) Check(ctx context.Context, cfg Config) Status {
	var st Status
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var v struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, cfg.URL, "/api/version", &v); err != nil {
		st.Error = err.Error()
		return st
	}
	st.Reachable, st.Version = true, v.Version
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := c.get(ctx, cfg.URL, "/api/tags", &tags); err != nil {
		st.Error = err.Error()
	}
	for _, m := range tags.Models {
		st.Models = append(st.Models, m.Name)
	}
	st.ChatInstalled = Installed(st.Models, cfg.ChatModel)
	st.EmbedInstalled = Installed(st.Models, cfg.EmbedModel)
	var ps struct {
		Models []struct {
			Name      string    `json:"name"`
			SizeVRAM  int64     `json:"size_vram"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"models"`
	}
	if err := c.get(ctx, cfg.URL, "/api/ps", &ps); err == nil {
		for _, m := range ps.Models {
			st.Loaded = append(st.Loaded, LoadedModel{Name: m.Name, VRAMBytes: m.SizeVRAM, ExpiresAt: m.ExpiresAt})
		}
	}
	return st
}

// Installed reports whether model is among names ("mxbai-embed-large" matches
// "mxbai-embed-large:latest").
func Installed(names []string, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, n := range names {
		if n == model || (!strings.Contains(model, ":") && n == model+":latest") {
			return true
		}
	}
	return false
}

// QueryPrefix is the instruction an embedding model wants in front of a search query
// (documents are embedded as they are, except for models that want a prefix on both).
func QueryPrefix(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "mxbai") || strings.Contains(m, "bge"):
		return "Represent this sentence for searching relevant passages: "
	case strings.Contains(m, "nomic"):
		return "search_query: "
	}
	return ""
}

// DocPrefix is the prefix for documents (only some models want one).
func DocPrefix(model string) string {
	if strings.Contains(strings.ToLower(model), "nomic") {
		return "search_document: "
	}
	return ""
}

// ModelID names an embedding model in stored rows, so a model change re-embeds.
func ModelID(model string) string { return "ollama/" + model }
