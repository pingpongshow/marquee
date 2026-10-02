package plex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"marquee/internal/auth"
	"marquee/internal/db"
	"marquee/internal/library"
)

type recordingMatcher struct{ calls []string }

func (m *recordingMatcher) MatchTo(_ context.Context, itemID int64, tmdbID int) error {
	m.calls = append(m.calls, fmt.Sprintf("%d→%d", itemID, tmdbID))
	return nil
}

// TestRealImport runs a full import of a real Plex database into a copy of a real Marquee
// database. Skipped unless PLEX_TEST_ROOT (a folder containing Plex's "Library") and
// MARQUEE_TEST_DB (a disposable copy of marquee.db) are set.
func TestRealImport(t *testing.T) {
	root, mdb := os.Getenv("PLEX_TEST_ROOT"), os.Getenv("MARQUEE_TEST_DB")
	if root == "" || mdb == "" {
		t.Skip("set PLEX_TEST_ROOT and MARQUEE_TEST_DB")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, mdb, filepath.Dir(mdb))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m := &recordingMatcher{}
	work := t.TempDir()
	im := &Importer{DB: d, Auth: auth.NewService(d), Libraries: library.NewStore(d), Matcher: m,
		PlexRoot: root, WorkDir: work, ArtDir: filepath.Join(work, "art")}
	pv, err := im.Preview(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("mappings: %+v", pv.Mappings)
	for _, s := range pv.Sections {
		t.Logf("section %-10s files %5d matched %5d → library %d", s.Name, s.Files, s.MatchedFiles, s.LibraryID)
	}
	t.Logf("total %d matched %d; unmatched sample: %q", pv.Files, pv.Matched, pv.UnmatchedSample)
	var choices []AccountChoice
	for _, a := range pv.Accounts {
		t.Logf("account %-12s id %-10d watched %5d inprog %3d hist %5d pl %2d → %s %d %q", a.Name, a.ID, a.Watched, a.InProgress, a.History, a.Playlists, a.SuggestedAction, a.SuggestedUserID, a.SuggestedUsername)
		choices = append(choices, AccountChoice{PlexID: a.ID, Action: a.SuggestedAction, UserID: a.SuggestedUserID, Username: a.SuggestedUsername})
	}
	opts := Options{Mappings: pv.Mappings, Accounts: choices, Matches: true, WatchState: true, History: true, Playlists: true, Markers: true, Artwork: true}
	rep, err := im.run(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("report: %+v", *rep)
	t.Logf("matches applied: %d %v", len(m.calls), m.calls)
	for _, k := range rep.MatchesKept {
		t.Logf("kept: %s", k)
	}
	for _, f := range rep.MatchFailures {
		t.Logf("not applied: %s", f)
	}

	// Idempotence: a second run must not duplicate anything.
	count := func(q string) (n int) { d.QueryRow(q).Scan(&n); return }
	before := []int{count(`SELECT COUNT(*) FROM play_history`), count(`SELECT COUNT(*) FROM playlists`), count(`SELECT COUNT(*) FROM users`), count(`SELECT COUNT(*) FROM markers WHERE source='plex'`), count(`SELECT COUNT(*) FROM user_item_state`)}
	if _, err := im.run(ctx, opts); err != nil {
		t.Fatal(err)
	}
	after := []int{count(`SELECT COUNT(*) FROM play_history`), count(`SELECT COUNT(*) FROM playlists`), count(`SELECT COUNT(*) FROM users`), count(`SELECT COUNT(*) FROM markers WHERE source='plex'`), count(`SELECT COUNT(*) FROM user_item_state`)}
	t.Logf("history/playlists/users/markers/state before %v after re-run %v", before, after)
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("re-run changed count %d: %d → %d", i, before[i], after[i])
		}
	}
	var _ sql.NullString
}
