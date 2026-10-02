package introdetect

import (
	"fmt"
	"strings"
	"testing"
)

func blackOutput(secs map[int]int) []byte {
	var b strings.Builder
	for s, p := range secs {
		fmt.Fprintf(&b, "[Parsed_blackframe_2 @ 0x1] frame:%d pblack:%d pts:%d t:%d.000000 type:P last_keyframe:0\n", s, p, s, s)
	}
	return []byte(b.String())
}

// Scattered dark shots, then a credits roll from 235 s to the end of a 300 s window (with
// a short logo card in the middle).
func TestCreditsRunFindsRollToEnd(t *testing.T) {
	secs := map[int]int{94: 87, 128: 99, 135: 90, 136: 89, 149: 82, 150: 82, 151: 89}
	for s := 235; s < 300; s++ {
		if s < 262 || s > 264 {
			secs[s] = 97
		}
	}
	r, ok := creditsRun(parseBlack(blackOutput(secs)), 300)
	if !ok || r.Start != 235 || r.End != 300 {
		t.Fatalf("got %+v %v", r, ok)
	}
}

func TestCreditsRunIgnoresShortDarkScenes(t *testing.T) {
	secs := map[int]int{}
	for s := 40; s < 52; s++ {
		secs[s] = 95 // a 12 s night shot
	}
	if r, ok := creditsRun(parseBlack(blackOutput(secs)), 300); ok {
		t.Fatalf("found credits %+v", r)
	}
}
