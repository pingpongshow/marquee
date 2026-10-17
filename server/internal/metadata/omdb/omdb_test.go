package omdb

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A request that fails before any answer doesn't carry the API key in its error.
func TestRequestErrorHidesKey(t *testing.T) {
	c := New("omdb-secret-9")
	c.BaseURL = "http://127.0.0.1:1/" // nothing listens
	c.HTTP.Timeout = time.Second
	_, err := c.ByIMDbID(context.Background(), "tt0000001")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "omdb-secret-9") {
		t.Fatalf("error shows the key: %v", err)
	}
}
