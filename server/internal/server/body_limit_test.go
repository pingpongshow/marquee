package server

import (
	"strings"
	"testing"
)

// API request bodies are bounded: an oversized one is refused, not read into memory.
func TestOversizedBodyRefused(t *testing.T) {
	h := newHarness(t)
	huge := strings.Repeat("a", maxBodyBytes+1)
	if code := h.do("POST", "/auth/login", map[string]any{"username": "x", "password": huge, "device": device}, nil); code != 400 {
		t.Errorf("oversized login: %d", code)
	}
	if code := h.do("POST", "/auth/login", map[string]any{"username": "x", "password": "short password", "device": device}, nil); code != 401 {
		t.Errorf("normal login: %d", code)
	}
}
