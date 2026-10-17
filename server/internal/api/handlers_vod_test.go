package api

import (
	"context"
	"path/filepath"
	"testing"

	"marquee/internal/auth"
	"marquee/internal/db"
	"marquee/internal/settings"
	"marquee/internal/vod"
)

// A movie source's error can name its address, credentials and all (an M3U link with a
// username and password): only admins see it in the status.
func TestVodStatusSourceErrorAdminOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	store, err := settings.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "p", Name: "IPTV", URL: "http://iptv.example/get.php?username=u&password=secret", Enabled: true}}
		return nil
	})
	const msg = `Get "http://iptv.example/get.php?username=u&password=secret": dial tcp: connection refused`
	if _, err := d.Exec(`INSERT INTO vod_sources(id, name, movies, error) VALUES('p', 'IPTV', 1, ?)`, msg); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{Settings: store, VOD: &vod.Service{DB: d, Settings: store, Streams: &vod.Streams{}}}
	status := func(u auth.User) VodStatus {
		t.Helper()
		resp, err := h.VodStatus(auth.WithSession(ctx, auth.Session{User: u}), VodStatusRequestObject{})
		if err != nil {
			t.Fatal(err)
		}
		st, ok := resp.(VodStatus200JSONResponse)
		if !ok || len(st.Sources) != 1 {
			t.Fatalf("status: %#v", resp)
		}
		return VodStatus(st)
	}
	if st := status(auth.User{ID: 2}); st.Sources[0].Error != nil {
		t.Errorf("a non-admin sees the source's error: %q", *st.Sources[0].Error)
	}
	if st := status(auth.User{ID: 1, IsAdmin: true}); st.Sources[0].Error == nil || *st.Sources[0].Error != msg {
		t.Errorf("admin: %+v", st.Sources[0])
	}
}
