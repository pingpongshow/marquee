package api

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
)

// Channel logos come from the Live TV source and may be SVG: served with a policy that
// keeps any script in them from running on the server's origin.
func TestLiveLogoPolicy(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	var resp LiveChannelLogoResponseObject = logoResponse{LiveChannelLogo200ImageResponse{Body: bytes.NewReader(svg), ContentType: "image/svg+xml", ContentLength: int64(len(svg))}}
	w := httptest.NewRecorder()
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'") // what the middleware sets
	if err := resp.VisitLiveChannelLogoResponse(w); err != nil {
		t.Fatal(err)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "sandbox") {
		t.Errorf("policy %q", csp)
	}
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/svg+xml" || !bytes.Equal(w.Body.Bytes(), svg) {
		t.Errorf("response %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}
