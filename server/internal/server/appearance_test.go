package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"marquee/internal/api"
	"marquee/internal/appearance"
)

// The page background and theme (USER-20): anyone can read them, only an admin can set them.
func TestAppearance(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	adminToken := res.Token
	h.token = adminToken
	var kid api.User
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true}, &kid)
	h.do("POST", "/auth/pin", map[string]any{"userId": kid.Id, "device": device}, &res)
	kidToken := res.Token

	// Without signing in: the defaults, and no image.
	h.token = ""
	var a api.Appearance
	if code := h.do("GET", "/appearance", nil, &a); code != 200 || a.ThemeId != "marquee" || a.ActiveThemeId != "marquee" || a.Background.ImageUrl != nil || a.Background.Mode != api.AppearanceBackgroundModeTile || a.Background.Dim != 40 {
		t.Fatalf("default: %d %+v", code, a)
	}
	if resp, _ := h.raw("GET", "/api/v1/appearance/background", nil); resp.StatusCode != 404 {
		t.Errorf("no image: %d", resp.StatusCode)
	}

	var pic bytes.Buffer
	png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 40, 40)))
	update := map[string]any{"themeId": "ocean", "autoHolidays": false, "backgroundMode": "fill", "backgroundDim": 30}

	// Signed out: 401. A non-admin: 403, for all three.
	if code := h.do("PUT", "/appearance", update, nil); code != 401 {
		t.Errorf("signed-out update: %d", code)
	}
	h.token = kidToken
	if code := h.do("PUT", "/appearance", update, nil); code != 403 {
		t.Errorf("non-admin update: %d", code)
	}
	if resp, _ := h.raw("PUT", "/api/v1/appearance/background", pic.Bytes()); resp.StatusCode != 403 {
		t.Errorf("non-admin upload: %d", resp.StatusCode)
	}
	if resp, _ := h.raw("DELETE", "/api/v1/appearance/background", nil); resp.StatusCode != 403 {
		t.Errorf("non-admin remove: %d", resp.StatusCode)
	}

	h.token = adminToken
	if code := h.do("PUT", "/appearance", update, &a); code != 200 || a.ActiveThemeId != "ocean" || a.Background.Dim != 30 || a.Background.Mode != api.AppearanceBackgroundModeFill {
		t.Fatalf("update: %d %+v", code, a)
	}
	// Dim is a strength towards the theme's background; a signed value means its size.
	if code := h.do("PUT", "/appearance", map[string]any{"backgroundDim": -60}, &a); code != 200 || a.Background.Dim != 60 {
		t.Errorf("signed dim: %d %d", code, a.Background.Dim)
	}
	for name, bad := range map[string]map[string]any{
		"unknown theme": {"themeId": "nope"},
		"mode":          {"backgroundMode": "stretch"},
		"dim":           {"backgroundDim": 500},
	} {
		if code := h.do("PUT", "/appearance", bad, nil); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}

	// An SVG is refused whatever it claims to be; nothing changes.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if resp, body := h.raw("PUT", "/api/v1/appearance/background", svg); resp.StatusCode != 400 || !strings.Contains(string(body), "invalid_image") {
		t.Errorf("svg: %d %s", resp.StatusCode, body)
	}

	resp, body := h.raw("PUT", "/api/v1/appearance/background", pic.Bytes())
	if resp.StatusCode != 200 {
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
	json.Unmarshal(body, &a)
	if a.Background.ImageUrl == nil {
		t.Fatalf("no image url: %s", body)
	}
	first := *a.Background.ImageUrl

	// The sign-in page shows it, so no token is needed, and the URL can be cached forever.
	h.token = ""
	resp, body = h.raw("GET", first, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("get image: %d %v", resp.StatusCode, resp.Header)
	}
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		t.Errorf("stored image: %v", err)
	}

	// A new upload gets a new URL.
	h.token = adminToken
	_, body = h.raw("PUT", "/api/v1/appearance/background", pic.Bytes())
	json.Unmarshal(body, &a)
	if *a.Background.ImageUrl == first {
		t.Error("the URL didn't change") // version is in milliseconds; a real upload takes longer
	}

	if resp, body := h.raw("DELETE", "/api/v1/appearance/background", nil); resp.StatusCode != 200 || strings.Contains(string(body), "imageUrl") {
		t.Errorf("remove: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.raw("GET", first, nil); resp.StatusCode != 404 {
		t.Errorf("image after remove: %d", resp.StatusCode)
	}
}

// The upload's limit is above other requests', up to the image limit.
func TestAppearanceUploadLimit(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	resp, body := h.raw(http.MethodPut, "/api/v1/appearance/background", bytes.Repeat([]byte{'a'}, appearance.MaxBytes+1))
	if resp.StatusCode != 400 || !strings.Contains(string(body), "larger than") {
		t.Errorf("too large: %d %s", resp.StatusCode, body)
	}
}
