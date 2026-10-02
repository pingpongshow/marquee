package tasks

import (
	"testing"
	"time"
)

func TestInWindow(t *testing.T) {
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 10, 2, h, m, 0, 0, loc) }
	cases := []struct {
		now   time.Time
		in    bool
		start time.Time
	}{
		{at(3, 30), true, at(3, 0)},
		{at(2, 59), false, at(3, 0).AddDate(0, 0, -1)},
		{at(7, 0), false, at(3, 0)},
		{at(6, 59), true, at(3, 0)},
	}
	for _, c := range cases {
		in, start := inWindow(c.now, "03:00", 4)
		if in != c.in || !start.Equal(c.start) {
			t.Errorf("%v: got %v %v, want %v %v", c.now, in, start, c.in, c.start)
		}
	}
	// A window that crosses midnight.
	if in, _ := inWindow(at(1, 0), "23:00", 4); !in {
		t.Error("01:00 should be inside a 23:00 + 4 h window")
	}
}
