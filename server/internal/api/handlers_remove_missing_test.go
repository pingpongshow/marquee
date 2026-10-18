package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/auth"
	"marquee/internal/db"
	"marquee/internal/library"
	"marquee/internal/settings"
)

// Removing missing titles is for admins; Settings → Libraries shows how many there are.
func TestRemoveMissingTitles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	root := filepath.Join(dir, "movies")
	os.MkdirAll(filepath.Join(root, "Heat (1995)"), 0o755)
	os.WriteFile(filepath.Join(root, "Heat (1995)", "Heat (1995).mkv"), []byte("x"), 0o644)
	libs := library.NewStore(d)
	l, err := libs.Create(ctx, "Movies", library.Movies, []string{root}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO items(id, library_id, type, title, sort_title, available) VALUES (1, ?1, 'movie', 'Heat', 'Heat', 1), (2, ?1, 'movie', 'Alien', 'Alien', 0)`,
		`INSERT INTO media_versions(id, item_id) VALUES (1, 1), (2, 2)`,
		`INSERT INTO media_files(version_id, library_id, path, size, mtime, available) VALUES
			(1, ?1, ?2 || '/Heat (1995)/Heat (1995).mkv', 1, 0, 1), (2, ?1, ?2 || '/Alien (1979)/Alien (1979).mkv', 1, 0, 0)`,
	} {
		if _, err := d.Exec(q, l.ID, root); err != nil {
			t.Fatal(err)
		}
	}
	st, err := settings.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: d, Libraries: libs, Settings: st}
	user := auth.WithSession(ctx, auth.Session{User: auth.User{ID: 2}})
	adm := auth.WithSession(ctx, auth.Session{User: auth.User{ID: 1, IsAdmin: true}})

	if r, _ := h.RemoveMissingTitles(user, RemoveMissingTitlesRequestObject{LibraryId: l.ID}); r == nil {
		t.Fatal("no response")
	} else if _, ok := r.(RemoveMissingTitles403JSONResponse); !ok {
		t.Errorf("as a user: %#v", r)
	}
	// Only admins managing libraries get the count.
	r, err := h.ListLibraries(user, ListLibrariesRequestObject{Params: ListLibrariesParams{Manage: ptr(true)}})
	if err != nil {
		t.Fatal(err)
	}
	if ls := r.(ListLibraries200JSONResponse); len(ls) != 1 || ls[0].MissingCount != nil {
		t.Errorf("user listing: %#v", ls)
	}
	if r, err = h.ListLibraries(adm, ListLibrariesRequestObject{Params: ListLibrariesParams{Manage: ptr(true)}}); err != nil {
		t.Fatal(err)
	}
	if ls := r.(ListLibraries200JSONResponse); len(ls) != 1 || ls[0].MissingCount == nil || *ls[0].MissingCount != 1 {
		t.Errorf("admin listing: %#v", ls)
	}

	res, err := h.RemoveMissingTitles(adm, RemoveMissingTitlesRequestObject{LibraryId: l.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := res.(RemoveMissingTitles200JSONResponse); ok.Removed != 1 {
		t.Fatalf("remove: %#v", res)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM items WHERE library_id = ?`, l.ID).Scan(&n)
	if n != 1 {
		t.Errorf("items left = %d, want Heat only", n)
	}
	if res, _ := h.RemoveMissingTitles(adm, RemoveMissingTitlesRequestObject{LibraryId: 99}); res == nil {
		t.Error("no response")
	} else if _, ok := res.(RemoveMissingTitles404JSONResponse); !ok {
		t.Errorf("unknown library: %#v", res)
	}
	// Nothing reachable: refused.
	os.Rename(root, root+"-offline")
	if res, _ := h.RemoveMissingTitles(adm, RemoveMissingTitlesRequestObject{LibraryId: l.ID}); res == nil {
		t.Error("no response")
	} else if _, ok := res.(RemoveMissingTitles409JSONResponse); !ok {
		t.Errorf("offline library: %#v", res)
	}
	// Settings → Libraries warns about the folder that can't be read.
	if r, err = h.ListLibraries(adm, ListLibrariesRequestObject{Params: ListLibrariesParams{Manage: ptr(true)}}); err != nil {
		t.Fatal(err)
	}
	if ls := r.(ListLibraries200JSONResponse); len(ls) != 1 || ls[0].UnreadablePaths == nil || len(*ls[0].UnreadablePaths) != 1 || (*ls[0].UnreadablePaths)[0] != root {
		t.Errorf("unreadable folder not reported: %#v", ls)
	}
	os.Rename(root+"-offline", root)
	if r, err = h.ListLibraries(adm, ListLibrariesRequestObject{Params: ListLibrariesParams{Manage: ptr(true)}}); err != nil {
		t.Fatal(err)
	}
	if ls := r.(ListLibraries200JSONResponse); ls[0].UnreadablePaths != nil {
		t.Errorf("readable folder reported: %v", *ls[0].UnreadablePaths)
	}
}
