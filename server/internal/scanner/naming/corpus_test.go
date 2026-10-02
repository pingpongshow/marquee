package naming

import (
	"bufio"
	"os"
	"path"
	"strings"
	"testing"
)

// TestCorpus runs the parser over real file lists (one relative path per line) and logs
// anything it can't parse. Skipped unless NAMING_MOVIES / NAMING_EPISODES are set:
//
//	NAMING_MOVIES=movies.txt NAMING_EPISODES=episodes.txt go test -run Corpus -v ./internal/scanner/naming
func TestCorpus(t *testing.T) {
	if f := os.Getenv("NAMING_MOVIES"); f != "" {
		bad := 0
		for _, p := range lines(t, f) {
			parts := strings.Split(p, "/")
			m := ParseMovie(parts[1], path.Base(p))
			if m.Title == "" || m.Year == 0 {
				bad++
				t.Logf("movie  %-70s → %q (%d)", p, m.Title, m.Year)
			}
		}
		t.Logf("movies without title or year: %d", bad)
	}
	if f := os.Getenv("NAMING_EPISODES"); f != "" {
		bad := 0
		for _, p := range lines(t, f) {
			if _, ok := ParseEpisode(path.Base(p)); !ok {
				bad++
				t.Logf("episode %s", p)
			}
		}
		t.Logf("unparsed episodes: %d", bad)
	}
}

func lines(t *testing.T, name string) []string {
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			out = append(out, l)
		}
	}
	return out
}
