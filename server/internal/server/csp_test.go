package server

import (
	"net/http"
	"strings"
	"testing"
)

// Every response carries the Content-Security-Policy: Marquee's own scripts only, no
// framing by other sites.
func TestContentSecurityPolicy(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/", "/api/v1/system/info"} {
		resp, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		csp := resp.Header.Get("Content-Security-Policy")
		for _, want := range []string{"script-src 'self' 'wasm-unsafe-eval'", "object-src 'none'", "frame-ancestors 'none'", "worker-src 'self' blob:"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: policy lacks %q: %s", path, want, csp)
			}
		}
		if strings.Contains(csp, "'unsafe-eval'") && !strings.Contains(csp, "'wasm-unsafe-eval'") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
			t.Errorf("%s: scripts not locked down: %s", path, csp)
		}
	}
}
