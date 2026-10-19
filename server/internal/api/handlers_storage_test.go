package api

import (
	"context"
	"path/filepath"
	"testing"

	"marquee/internal/auth"
	"marquee/internal/db"
	"marquee/internal/storage"
)

// The storage overview is for admins; it lists libraries (recordings ones marked), the
// biggest titles and the history.
func TestStorageReport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, q := range []string{
		`INSERT INTO libraries(id, name, type, recordings, sort_order) VALUES (1, 'Movies', 'movies', 0, 0), (2, 'DVR', 'shows', 1, 1)`,
		`INSERT INTO items(id, library_id, type, title, sort_title, year) VALUES (1, 1, 'movie', 'Heat', 'Heat', 1995)`,
		`INSERT INTO media_versions(id, item_id) VALUES (1, 1)`,
		`INSERT INTO media_files(version_id, library_id, path, size, mtime) VALUES (1, 1, '/m/heat.mkv', 1234, 0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	svc := &storage.Service{DB: d}
	if _, err := svc.Sample(ctx); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: d, Storage: svc}
	user := auth.WithSession(ctx, auth.Session{User: auth.User{ID: 2}})
	adm := auth.WithSession(ctx, auth.Session{User: auth.User{ID: 1, IsAdmin: true}})

	if r, _ := h.StorageReport(user, StorageReportRequestObject{}); r == nil {
		t.Fatal("no response")
	} else if _, ok := r.(StorageReport403JSONResponse); !ok {
		t.Errorf("as a user: %#v", r)
	}
	r, err := h.StorageReport(adm, StorageReportRequestObject{Params: StorageReportParams{Days: ptr(30), Top: ptr(5)}})
	if err != nil {
		t.Fatal(err)
	}
	rep, ok := r.(StorageReport200JSONResponse)
	if !ok {
		t.Fatalf("as an admin: %#v", r)
	}
	if len(rep.Libraries) != 2 || rep.Libraries[0].Bytes != 1234 || rep.Libraries[0].Files != 1 || len(rep.Libraries[0].History) != 1 ||
		rep.Libraries[0].History[0].Bytes != 1234 || rep.Libraries[1].Name != "DVR (recordings)" {
		t.Errorf("libraries: %+v", rep.Libraries)
	}
	if len(rep.Largest) != 1 || rep.Largest[0].ItemId != 1 || rep.Largest[0].Year == nil || *rep.Largest[0].Year != 1995 {
		t.Errorf("largest: %+v", rep.Largest)
	}
	if rep.Volumes == nil {
		t.Error("volumes should be an empty list, not null")
	}
}
