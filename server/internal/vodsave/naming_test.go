package vodsave

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The library's Radarr/Sonarr layout.
func TestNamingPath(t *testing.T) {
	long := strings.Repeat("Very Long Title ", 30)
	tests := []struct {
		name string
		n    Naming
		want string
	}{
		{"film", Naming{Kind: "movie", Root: "/m/Movies", Title: "(500) Days of Summer", Year: 2009, Width: 1920, Height: 1080, Ext: "mp4"},
			"/m/Movies/(500) Days of Summer (2009)/(500) Days of Summer (2009) WEBDL-1080p.mp4"},
		{"letterboxed film is 1080p by width", Naming{Kind: "movie", Root: "/m/Movies", Title: "Wuthering Heights", Year: 2026, Width: 1920, Height: 800, Ext: "mkv"},
			"/m/Movies/Wuthering Heights (2026)/Wuthering Heights (2026) WEBDL-1080p.mkv"},
		{"film in an existing folder", Naming{Kind: "movie", Root: "/m/Movies", Folder: "/m/Movies/wuthering heights (2026)", Title: "Wuthering Heights", Year: 2026, Height: 720, Ext: "mkv"},
			"/m/Movies/wuthering heights (2026)/Wuthering Heights (2026) WEBDL-720p.mkv"},
		{"unknown resolution: no tag", Naming{Kind: "movie", Root: "/m", Title: "Film", Year: 2001, Ext: "mkv"}, "/m/Film (2001)/Film (2001).mkv"},
		{"no year", Naming{Kind: "movie", Root: "/m", Title: "Film", Height: 480, Ext: "avi"}, "/m/Film/Film WEBDL-480p.avi"},
		{"episode", Naming{Kind: "episode", Root: "/m/Shows", Show: "Archer", Year: 2009, Season: 1, Episode: 1, Title: "Mole Hunt", Width: 1920, Height: 1080, Ext: "mkv"},
			"/m/Shows/Archer (2009)/Season 1/Archer - S01E01 - Mole Hunt WEBDL-1080p.mkv"},
		{"episode in an old folder without the year", Naming{Kind: "episode", Root: "/m/Shows", Folder: "/m/Shows/Archer", Show: "Archer", Year: 2009, Season: 12, Episode: 3, Title: "X", Height: 720, Ext: "mkv"},
			"/m/Shows/Archer/Season 12/Archer - S12E03 - X WEBDL-720p.mkv"},
		{"unknown episode title", Naming{Kind: "episode", Root: "/s", Show: "Show", Year: 2020, Season: 1, Episode: 5, Ext: "mkv"},
			"/s/Show (2020)/Season 1/Show - S01E05.mkv"},
		{"special", Naming{Kind: "episode", Root: "/s", Show: "Show", Season: 0, Episode: 2, Title: "Pilot", Height: 576, Ext: "mkv"},
			"/s/Show/Season 0/Show - S00E02 - Pilot WEBDL-576p.mkv"},
		{"colons and illegal characters", Naming{Kind: "episode", Root: "/s", Show: "Alien: Earth", Year: 2025, Season: 1, Episode: 1, Title: `What? "Neverland" <1/2>`, Height: 2160, Ext: "mkv"},
			"/s/Alien - Earth (2025)/Season 1/Alien - Earth - S01E01 - What Neverland 12 WEBDL-2160p.mkv"},
		{"unicode and curly quotes stay", Naming{Kind: "movie", Root: "/m", Title: "Amélie’s “Fabuleux” Destin", Year: 2001, Height: 1080, Ext: "mkv"},
			"/m/Amélie’s “Fabuleux” Destin (2001)/Amélie’s “Fabuleux” Destin (2001) WEBDL-1080p.mkv"},
	}
	for _, tc := range tests {
		if got := tc.n.Path(); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
	// Long names are cut to fit, on a character.
	p := Naming{Kind: "movie", Root: "/m", Title: long + "é", Year: 2000, Height: 1080, Ext: "mkv"}.Path()
	for _, part := range strings.Split(p, "/") {
		if len(part) > 200 || !utf8.ValidString(part) {
			t.Errorf("component too long or broken: %d %q", len(part), part)
		}
	}
	ep := Naming{Kind: "episode", Root: "/s", Show: long, Season: 1, Episode: 1, Title: long + "ü", Height: 1080, Ext: "mkv"}.Path()
	if base := filepath.Base(ep); len(base) > 200 || !strings.HasSuffix(base, " WEBDL-1080p.mkv") || !strings.Contains(base, "S01E01") {
		t.Errorf("long episode: %d %q", len(base), base)
	}
}

func TestResolution(t *testing.T) {
	for _, tc := range []struct{ w, h, want int }{
		{1920, 1080, 1080}, {1920, 800, 1080}, {1920, 1040, 1080}, {3840, 1600, 2160}, {3840, 2160, 2160},
		{1280, 720, 720}, {1280, 536, 720}, {720, 576, 576}, {720, 480, 480}, {640, 360, 480}, {1440, 1080, 1080}, {0, 0, 0},
	} {
		if got := Resolution(tc.w, tc.h); got != tc.want {
			t.Errorf("%dx%d: %d, want %d", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		year int
	}{
		{"EN - The Matrix (1999)", "The Matrix", 1999},
		{"|EN| Alien: Earth", "Alien: Earth", 0},
		{"[4K] Dune Part Two (2024) (2024)", "Dune Part Two", 2024},
		{"The Bear HEVC MULTI-SUB", "The Bear", 0},
		{"4K | Oppenheimer [HDR] (2023)", "Oppenheimer", 2023},
		{"Blade Runner 2049 (2017)", "Blade Runner 2049", 2017},
		{"1917 (2019)", "1917", 2019},
		{"M*A*S*H", "M*A*S*H", 0},
		{"Avatar: The Last Airbender", "Avatar: The Last Airbender", 0},
	} {
		got, year := CleanTitle(tc.in)
		if got != tc.want || year != tc.year {
			t.Errorf("%q: %q %d, want %q %d", tc.in, got, year, tc.want, tc.year)
		}
	}
	if got := Sanitize("Avatar: The Last Airbender", 180); got != "Avatar - The Last Airbender" {
		t.Errorf("colon: %q", got)
	}
	if got := Sanitize("CON", 180); got != "_CON" {
		t.Errorf("reserved: %q", got)
	}
	if got := Sanitize(" ...Trailing dots... ", 180); got != "Trailing dots" {
		t.Errorf("dots: %q", got)
	}
}

func TestFindFolderAndNoOverwrite(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Archer"), 0o755)          // older, no year
	os.MkdirAll(filepath.Join(root, "THE BEAR (2022)"), 0o755) // other spelling
	ctx := context.Background()
	if got := FindFolder(ctx, nil, "show", root, "Archer", 2009, 0); got != filepath.Join(root, "Archer") {
		t.Errorf("archer: %q", got)
	}
	if got := FindFolder(ctx, nil, "show", root, "The Bear", 2022, 0); got != filepath.Join(root, "THE BEAR (2022)") {
		t.Errorf("bear: %q", got)
	}
	if got := FindFolder(ctx, nil, "show", root, "Severance", 2022, 0); got != "" {
		t.Errorf("none: %q", got)
	}
	f := filepath.Join(root, "a.mkv")
	os.WriteFile(f, []byte("x"), 0o644)
	if got := freeName(f); got != filepath.Join(root, "a (2).mkv") {
		t.Errorf("free name: %q", got)
	}
	src := filepath.Join(root, "new.part")
	os.WriteFile(src, []byte("new"), 0o644)
	got, err := place(src, f)
	if err != nil || got != filepath.Join(root, "a (2).mkv") {
		t.Fatalf("place: %q %v", got, err)
	}
	if b, _ := os.ReadFile(f); string(b) != "x" {
		t.Error("the existing file was overwritten")
	}
	if st, _ := os.Stat(got); st.Mode().Perm() != 0o664 {
		t.Errorf("mode %v", st.Mode())
	}
}
