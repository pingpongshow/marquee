package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Making clients leaves nothing running (each used to start a ticker goroutine that never
// stopped), and clients for other languages share the rate limit.
func TestClientsLeaveNoGoroutinesAndShareTheLimit(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 50 {
		New("k", "en")
	}
	time.Sleep(10 * time.Millisecond)
	if n := runtime.NumGoroutine(); n > before {
		t.Fatalf("%d goroutines after making 50 clients, %d before", n, before)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	en := New("k", "en")
	en.BaseURL = srv.URL
	ja := en.WithLanguage("ja")
	start := time.Now()
	for i := range 4 {
		c := en
		if i%2 == 1 {
			c = ja
		}
		var out map[string]any
		if err := c.get(context.Background(), "/x", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	// Four requests at 50 ms spacing take at least 150 ms, whichever language asks.
	if d := time.Since(start); d < 140*time.Millisecond {
		t.Errorf("4 requests took %v; the rate limit isn't shared", d)
	}
}

// A request that fails before any answer doesn't carry the API key in its error.
func TestRequestErrorHidesKey(t *testing.T) {
	c := New("secret-key-123", "")
	c.BaseURL = "http://127.0.0.1:1" // nothing listens
	c.HTTP.Timeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out map[string]any
	err := c.get(ctx, "/movie/1", nil, &out)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "secret-key-123") {
		t.Fatalf("error shows the key: %v", err)
	}
}
