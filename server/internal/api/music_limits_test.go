package api

import "testing"

func TestClampCount(t *testing.T) {
	for _, c := range []struct{ n, want int }{{-5, 3}, {0, 3}, {3, 3}, {15, 15}, {60, 60}, {1 << 30, 60}} {
		if got := clampCount(c.n, 3, 60); got != c.want {
			t.Errorf("clampCount(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestUpperFirst(t *testing.T) {
	for in, want := range map[string]string{"chill": "Chill", "électro": "Électro", "ñu": "Ñu", "": "", "1990s": "1990s"} {
		if got := upperFirst(in); got != want {
			t.Errorf("upperFirst(%q) = %q, want %q", in, got, want)
		}
	}
}
