package items

import (
	"context"
	"testing"
	"time"
)

// Two changes in the same second keep their order (variable-width stamps sorted "…:06Z"
// after "…:06.5Z", so the newer change was skipped).
func TestFreshOrdersWithinASecond(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	uid := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	movie := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Heat', 'Heat')`, lib)
	t0 := time.Date(2026, 10, 11, 4, 25, 6, 0, time.UTC)
	for _, c := range []struct {
		at   time.Time
		want bool
	}{{t0, true}, {t0.Add(500 * time.Millisecond), true}, {t0.Add(200 * time.Millisecond), false}, {t0.Add(time.Second), true}} {
		at := c.at
		got, err := s.Fresh(ctx, uid, movie, "watched", &at)
		if err != nil || got != c.want {
			t.Fatalf("%s: fresh=%v err=%v, want %v", c.at.Format(time.RFC3339Nano), got, err, c.want)
		}
	}
}
