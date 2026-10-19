package api

import (
	"context"
	"path/filepath"
	"testing"

	"marquee/internal/auth"
	"marquee/internal/db"
	"marquee/internal/settings"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// Following a series needs following on, on-demand access and the may-record permission;
// people see their own follows (admins everyone's) and only admins remove someone else's.
func TestFollowVodSeriesPermissions(t *testing.T) {
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
		s.Integrations.VODSources = []settings.VODSource{{ID: "xc", Name: "Dispatcharr", Kind: settings.VODKindXtream,
			URL: "http://127.0.0.1:1", Username: "u", Password: "p", Enabled: true}}
		s.VODSave.ShowsFolder, s.VODSave.FollowDefaultMaxGB = dir, 20
		return nil
	})
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, is_admin) VALUES (1, 'admin', 'Admin', 1), (2, 'kim', 'Kim', 0), (3, 'lee', 'Lee', 0)`,
		`INSERT INTO vod_series (id, source_id, source_key, title, added_at) VALUES (5, 'xc', 'xtream:201', 'Kings', '2026-10-01T00:00:00Z')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	v := &vod.Service{DB: d, Settings: store, Streams: &vod.Streams{}}
	h := &Handlers{Settings: store, VOD: v, VODSaves: &vodsave.Service{DB: d, Settings: store, VOD: v}}
	no := false
	admin := auth.User{ID: 1, IsAdmin: true}
	kim := auth.User{ID: 2, DisplayName: "Kim"}
	lee := auth.User{ID: 3, DisplayName: "Lee"}
	as := func(u auth.User) context.Context { return auth.WithSession(ctx, auth.Session{User: u}) }
	follow := func(u auth.User) FollowVodSeriesResponseObject {
		t.Helper()
		resp, err := h.FollowVodSeries(as(u), FollowVodSeriesRequestObject{SeriesId: 5, Body: &VodFollowRequest{IncludeExisting: ptr(true)}})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Off: 409; and the show page doesn't offer it.
	if r, ok := follow(kim).(FollowVodSeries409JSONResponse); !ok || r.Code != "follow_off" {
		t.Fatalf("off: %#v", follow(kim))
	}
	if f := h.vodFollowing(ctx, auth.Session{User: kim}, 5, "xc"); f != nil {
		t.Errorf("offered while off: %v", *f)
	}
	store.Update(ctx, func(s *settings.Settings) error { s.VODSave.FollowEnabled = true; return nil })
	// Not allowed to record, or no on-demand access: 403.
	if _, ok := follow(auth.User{ID: 2, Restrictions: auth.Restrictions{RecordDVR: &no}}).(FollowVodSeries403JSONResponse); !ok {
		t.Error("followed without the may-record permission")
	}
	if _, ok := follow(auth.User{ID: 2, Restrictions: auth.Restrictions{LiveTV: &no}}).(FollowVodSeries403JSONResponse); !ok {
		t.Error("followed without on-demand access")
	}
	if f := h.vodFollowing(ctx, auth.Session{User: kim}, 5, "xc"); f == nil || *f {
		t.Errorf("not offered: %v", f)
	}
	r, ok := follow(kim).(FollowVodSeries200JSONResponse)
	if !ok || r.Status != "following" || r.MaxGb == nil || *r.MaxGb != 20 || r.UserName == nil || *r.UserName != "Kim" {
		t.Fatalf("follow: %#v", follow(kim))
	}
	if f := h.vodFollowing(ctx, auth.Session{User: kim}, 5, "xc"); f == nil || !*f {
		t.Errorf("following: %v", f)
	}
	list := func(u auth.User, all bool) ListVodFollowsResponseObject {
		resp, err := h.ListVodFollows(as(u), ListVodFollowsRequestObject{Params: ListVodFollowsParams{All: &all}})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if _, ok := list(kim, true).(ListVodFollows403JSONResponse); !ok {
		t.Error("a non-admin listed everyone's follows")
	}
	if l, _ := list(kim, false).(ListVodFollows200JSONResponse); len(l) != 1 || l[0].SeriesId != 5 {
		t.Errorf("own: %#v", l)
	}
	if l, _ := list(lee, false).(ListVodFollows200JSONResponse); len(l) != 0 {
		t.Errorf("someone else's: %#v", l)
	}
	if l, _ := list(admin, true).(ListVodFollows200JSONResponse); len(l) != 1 {
		t.Errorf("all: %#v", l)
	}
	unfollow := func(u auth.User, userID *int64) UnfollowVodSeriesResponseObject {
		resp, err := h.UnfollowVodSeries(as(u), UnfollowVodSeriesRequestObject{SeriesId: 5, Params: UnfollowVodSeriesParams{UserId: userID}})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	kimID := int64(2)
	if _, ok := unfollow(lee, &kimID).(UnfollowVodSeries404JSONResponse); !ok {
		t.Error("a non-admin removed someone else's follow")
	}
	if _, ok := unfollow(admin, &kimID).(UnfollowVodSeries204Response); !ok {
		t.Error("the admin couldn't remove it")
	}
	if _, ok := unfollow(kim, nil).(UnfollowVodSeries404JSONResponse); !ok {
		t.Error("unfollowed twice")
	}
}
