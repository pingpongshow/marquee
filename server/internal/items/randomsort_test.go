package items

import (
	"context"
	"fmt"
	"testing"
)

// A seeded random sort gives the same shuffle on every page, so paging through it shows
// each title exactly once; another seed gives another order.
func TestSeededRandomPaging(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	for i := 0; i < 35; i++ {
		title := fmt.Sprintf("Film %02d", i)
		mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, available) VALUES (?, 'movie', ?, ?, 1)`, lib, title, title)
	}
	order := func(seed int64) []int64 {
		t.Helper()
		var ids []int64
		for off := 0; off < 35; off += 10 {
			list, total, err := s.List(ctx, Unrestricted, lib, "movie", "random", Filter{Seed: seed}, off, 10)
			if err != nil || total != 35 {
				t.Fatalf("page %d: %v total %d", off, err, total)
			}
			for _, it := range list {
				ids = append(ids, it.ID)
			}
		}
		return ids
	}
	a := order(12345)
	seen := map[int64]bool{}
	for _, id := range a {
		if seen[id] {
			t.Fatalf("title %d shown twice", id)
		}
		seen[id] = true
	}
	if len(seen) != 35 {
		t.Fatalf("saw %d of 35 titles", len(seen))
	}
	if b := order(12345); fmt.Sprint(a) != fmt.Sprint(b) {
		t.Error("the same seed gave a different order")
	}
	if c := order(987654321); fmt.Sprint(a) == fmt.Sprint(c) {
		t.Error("another seed gave the same order")
	}
}
