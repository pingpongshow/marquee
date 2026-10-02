package auth

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

func TestImageKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (7, 'u', 'U')`)
	s := NewService(d)
	key := s.ImageKey(ctx, 7)
	if sess, ok := s.ResolveImageKey(ctx, key); !ok || sess.User.ID != 7 {
		t.Fatalf("own key: %v %+v", ok, sess)
	}
	// The same key after a restart (the secret is stored).
	if again := NewService(d).ImageKey(ctx, 7); again != key {
		t.Errorf("key changed: %s vs %s", again, key)
	}
	for _, bad := range []string{"8" + key[1:], key[:len(key)-1] + "0", "7.", "x", ""} {
		if _, ok := s.ResolveImageKey(ctx, bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	for path, want := range map[string]bool{
		"/api/v1/images/12?w=200": true, "/api/v1/people/3/photo": true, "/api/v1/users/7/avatar": true,
		"/api/v1/items/12": false, "/api/v1/users/7": false, "/api/v1/stream/abc/file": false,
	} {
		if got := IsImageRequest(httptest.NewRequest("GET", path, nil)); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
	if IsImageRequest(httptest.NewRequest("DELETE", "/api/v1/users/7/avatar", nil)) {
		t.Error("only reads")
	}
}
