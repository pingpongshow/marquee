package items

import (
	"context"
	"testing"
)

// Titles with every word lead; titles with only some of the words follow, so "willy wonka"
// finds "Wonka". Stop words alone match nothing.
func TestSearchMatchesAnyWord(t *testing.T) {
	d := testDB(t)
	s := NewStore(d)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	for _, title := range []string{"Wonka", "Willy Wonka & the Chocolate Factory", "The Matrix", "Free Willy"} {
		mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', ?, ?)`, lib, title, title)
	}
	titles := func(q string) []string {
		groups, err := s.Search(context.Background(), Access{}, q, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, g := range groups {
			for _, it := range g.Items {
				out = append(out, it.Title)
			}
		}
		return out
	}
	got := titles("willy wonka")
	if len(got) != 3 || got[0] != "Willy Wonka & the Chocolate Factory" {
		t.Fatalf("willy wonka: %q", got)
	}
	if got := titles("the wonka"); len(got) != 2 {
		t.Fatalf("the wonka should find both Wonka films and not The Matrix: %q", got)
	}
}
