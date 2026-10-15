package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOllama answers like Ollama 0.x: chat replies with reply, embed with one vector per input.
func fakeOllama(t *testing.T, reply string, seen *[]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&in)
			if seen != nil {
				*seen = append(*seen, in)
			}
		}
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0.40.0"}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3:8b"},{"name":"mxbai-embed-large:latest"}]}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[{"name":"qwen3:8b","size_vram":6000000000,"expires_at":"2026-01-01T00:05:00Z"}]}`))
		case "/api/chat":
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": reply}})
		case "/api/embed":
			n := len(in["input"].([]any))
			out := make([][]float32, n)
			for i := range out {
				out[i] = []float32{float32(i + 1), 0, 0}
			}
			json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
		default:
			http.NotFound(w, r)
		}
	}))
}

func cfgFor(url string) func() Config {
	return func() Config {
		return Config{Enabled: true, URL: url, ChatModel: "qwen3:8b", EmbedModel: "mxbai-embed-large", UnloadMinutes: 5}
	}
}

func TestChatStructured(t *testing.T) {
	var seen []map[string]any
	srv := fakeOllama(t, `{"genres":["Horror"],"title":"Scary nights"}`, &seen)
	defer srv.Close()
	c := &Client{Config: cfgFor(srv.URL)}
	var out struct {
		Genres []string `json:"genres"`
		Title  string   `json:"title"`
	}
	schema := map[string]any{"type": "object"}
	if err := c.Chat(context.Background(), "sys", "user", schema, &out); err != nil {
		t.Fatal(err)
	}
	if out.Title != "Scary nights" || len(out.Genres) != 1 {
		t.Fatalf("got %+v", out)
	}
	req := seen[0]
	if req["think"] != false || req["stream"] != false || req["keep_alive"] != "5m" || req["model"] != "qwen3:8b" {
		t.Errorf("request %+v", req)
	}
	if req["format"] == nil {
		t.Error("no format schema sent")
	}
	if temp := req["options"].(map[string]any)["temperature"].(float64); temp > 0.3 {
		t.Errorf("temperature %v", temp)
	}
}

func TestChatBadJSON(t *testing.T) {
	srv := fakeOllama(t, `not json`, nil)
	defer srv.Close()
	c := &Client{Config: cfgFor(srv.URL)}
	var out map[string]any
	if err := c.Chat(context.Background(), "s", "u", map[string]any{}, &out); err == nil {
		t.Fatal("want error for non-JSON answer")
	}
}

func TestEmbedBatch(t *testing.T) {
	var seen []map[string]any
	srv := fakeOllama(t, "", &seen)
	defer srv.Close()
	c := &Client{Config: cfgFor(srv.URL)}
	vs, err := c.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil || len(vs) != 3 || vs[2][0] != 3 {
		t.Fatalf("got %v %v", vs, err)
	}
	if seen[0]["keep_alive"] != "5m" || seen[0]["model"] != "mxbai-embed-large" {
		t.Errorf("request %+v", seen[0])
	}
}

func TestDisabled(t *testing.T) {
	c := &Client{Config: func() Config { return Config{URL: "http://127.0.0.1:1"} }}
	if _, err := c.Embed(context.Background(), []string{"x"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err %v", err)
	}
	var nilClient *Client
	if nilClient.Enabled() {
		t.Fatal("nil client enabled")
	}
}

func TestCircuitBreakerFailsFast(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &Client{Config: cfgFor(srv.URL), Now: func() time.Time { return now }}
	for i := 0; i < breakerAfter; i++ {
		if _, err := c.Embed(context.Background(), []string{"x"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if c.Ready() {
		t.Fatal("breaker should be open")
	}
	start := time.Now()
	if _, err := c.Embed(context.Background(), []string{"x"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
	if calls.Load() != breakerAfter {
		t.Fatalf("open breaker still called Ollama (%d calls)", calls.Load())
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("open breaker wasn't fast")
	}
	if msg, _, paused := c.LastError(); msg == "" || !paused {
		t.Fatalf("last error %q paused %v", msg, paused)
	}
	// After the cool-off it tries again, and a success closes it.
	now = now.Add(breakerBase + time.Second)
	if !c.Ready() {
		t.Fatal("should retry after the cool-off")
	}
}

func TestUnreachableIsQuick(t *testing.T) {
	c := &Client{Config: cfgFor("http://127.0.0.1:1"), ChatTimeout: time.Second}
	var out map[string]any
	if err := c.Chat(context.Background(), "s", "u", map[string]any{}, &out); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
}

func TestCheckAndInstalled(t *testing.T) {
	srv := fakeOllama(t, "", nil)
	defer srv.Close()
	c := &Client{}
	st := c.Check(context.Background(), Config{URL: srv.URL, ChatModel: "qwen3:8b", EmbedModel: "mxbai-embed-large"})
	if !st.Reachable || st.Version != "0.40.0" || !st.ChatInstalled || !st.EmbedInstalled || len(st.Loaded) != 1 {
		t.Fatalf("status %+v", st)
	}
	st = c.Check(context.Background(), Config{URL: srv.URL, ChatModel: "qwen3:32b", EmbedModel: "nomic-embed-text"})
	if st.ChatInstalled || st.EmbedInstalled {
		t.Fatalf("missing models reported installed: %+v", st)
	}
}

func TestSwitchEmbedder(t *testing.T) {
	var seen []map[string]any
	srv := fakeOllama(t, "", &seen)
	defer srv.Close()
	on := true
	c := &Client{Config: func() Config {
		cfg := cfgFor(srv.URL)()
		cfg.Enabled = on
		return cfg
	}}
	fb := fallbackEmbedder{}
	s := &SwitchEmbedder{AI: c, Fallback: fb}
	model, _, err := s.EmbedDocs(context.Background(), nil, "doc")
	if err != nil || model != "ollama/mxbai-embed-large" {
		t.Fatalf("model %q %v", model, err)
	}
	if _, _, err := s.EmbedDocs(context.Background(), []string{"space"}, "query"); err != nil {
		t.Fatal(err)
	}
	last := seen[len(seen)-1]["input"].([]any)[0].(string)
	if last != QueryPrefix("mxbai-embed-large")+"space" {
		t.Errorf("query sent as %q", last)
	}
	on = false
	if model, _, _ := s.EmbedDocs(context.Background(), nil, "doc"); model != "bge" {
		t.Fatalf("with AI off want the fallback, got %q", model)
	}
}

type fallbackEmbedder struct{}

func (fallbackEmbedder) EmbedDocs(_ context.Context, texts []string, _ string) (string, [][]float32, error) {
	return "bge", make([][]float32, len(texts)), nil
}

// A cold model load that outlasts the request's budget gives up in time, doesn't count as
// Ollama failing, and keeps loading the models in the background.
func TestBudgetWhileLoading(t *testing.T) {
	var warmed atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/generate":
			warmed.Add(1)
			w.Write([]byte(`{}`))
		case "/api/chat":
			select { // "loading": answers only after the caller gave up
			case <-release:
			case <-r.Context().Done():
			}
		default:
			w.Write([]byte(`{"embeddings":[[1]]}`))
		}
	}))
	defer srv.Close()
	defer close(release)
	c := &Client{Config: cfgFor(srv.URL)}
	ctx := WithBudget(context.Background(), 300*time.Millisecond)
	start := time.Now()
	var out map[string]any
	for i := 0; i < breakerAfter+1; i++ {
		if err := c.Chat(ctx, "s", "u", map[string]any{}, &out); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %s, past the budget", d)
	}
	if !c.Ready() {
		t.Fatal("running out of budget shouldn't pause AI")
	}
	deadline := time.Now().Add(2 * time.Second)
	for warmed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if warmed.Load() == 0 {
		t.Fatal("the model wasn't loaded in the background")
	}
	// Calls without a budget keep their own timeout.
	if d, b := limit(context.Background(), time.Minute); d != time.Minute || b {
		t.Fatalf("limit without budget: %s %v", d, b)
	}
}
