package introdetect

import (
	"math"
	"math/rand/v2"
	"testing"
)

func noise(r *rand.Rand, n int) []uint32 {
	out := make([]uint32, n)
	for i := range out {
		out[i] = r.Uint32()
	}
	return out
}

// A 50 s theme placed at different offsets in two episodes, with bit noise on a third of
// its points and dropouts,
// is found at the right place in both.
func TestMatchFindsSharedSegment(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	theme := noise(r, 404) // ≈ 50 s
	a, b := noise(r, 4800), noise(r, 4800)
	copy(a[100:], theme)
	for i, v := range theme {
		switch {
		case i%37 == 0:
			b[300+i] = r.Uint32() // dropout
		case i%3 == 0:
			b[300+i] = v ^ (1 << (i % 32)) ^ (1 << ((i + 7) % 32)) // two flipped bits
		default:
			b[300+i] = v
		}
	}
	ra, rb, ok := Match(a, b, 15, 3.5)
	if !ok {
		t.Fatal("no match")
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1 }
	if !near(ra.Start, 100*PointSeconds) || !near(rb.Start, 300*PointSeconds) || !near(ra.Len(), 404*PointSeconds) {
		t.Fatalf("got a=%+v b=%+v", ra, rb)
	}
}

func TestMatchRejectsUnrelatedAudio(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	if _, _, ok := Match(noise(r, 3000), noise(r, 3000), 15, 3.5); ok {
		t.Fatal("unrelated fingerprints matched")
	}
}
