// Package introdetect finds intros and end credits in TV episodes (PLAY-12) by comparing
// Chromaprint audio fingerprints of episodes in the same season: a stretch of audio that
// two episodes share near their start is the intro, and near their end, the credits. This
// is the approach of Jellyfin's Intro Skipper plugin.
package introdetect

import "math/bits"

// PointSeconds is how much audio one Chromaprint point covers.
const PointSeconds = 0.1238

const (
	// maxBitErrors is how many of a point's 32 bits may differ for two points to match.
	maxBitErrors = 6
	// valueSpread widens the inverted-index lookup to nearby values, which finds
	// alignments even when the exact point value isn't shared.
	valueSpread = 2
)

// Range is a span of seconds from the start of a fingerprint.
type Range struct{ Start, End float64 }

func (r Range) Len() float64 { return r.End - r.Start }

// Match finds the longest stretch of audio the two fingerprints share. Matching points may
// be up to maxGap seconds apart and still count as one stretch. ok is false when nothing
// shared is at least minLen seconds long.
func Match(a, b []uint32, minLen, maxGap float64) (ra, rb Range, ok bool) {
	if len(a) == 0 || len(b) == 0 {
		return
	}
	index := make(map[uint32]int, len(b))
	for i, v := range b {
		index[v] = i
	}
	shifts := map[int]struct{}{}
	for i, v := range a {
		for d := -valueSpread; d <= valueSpread; d++ {
			if j, found := index[v+uint32(d)]; found {
				shifts[j-i] = struct{}{}
			}
		}
	}
	gap := int(maxGap / PointSeconds)
	bestLen, bestStart, bestEnd, bestShift := 0, 0, 0, 0
	for shift := range shifts {
		start, end, n := longestRun(a, b, shift, gap)
		if n > bestLen {
			bestLen, bestStart, bestEnd, bestShift = n, start, end, shift
		}
	}
	if bestLen == 0 {
		return
	}
	ra = Range{float64(bestStart) * PointSeconds, float64(bestEnd+1) * PointSeconds}
	rb = Range{float64(bestStart+bestShift) * PointSeconds, float64(bestEnd+1+bestShift) * PointSeconds}
	return ra, rb, ra.Len() >= minLen
}

// longestRun compares a[i] with b[i+shift] and returns the longest run of matching points
// (allowing gaps of up to gap points) as indices into a, and its length in points.
func longestRun(a, b []uint32, shift, gap int) (start, end, length int) {
	runStart, last := -1, -1
	for i := max(0, -shift); i < len(a) && i+shift < len(b); i++ {
		if bits.OnesCount32(a[i]^b[i+shift]) > maxBitErrors {
			continue
		}
		if runStart < 0 || i-last > gap {
			runStart = i
		}
		last = i
		if n := last - runStart + 1; n > length {
			start, end, length = runStart, last, n
		}
	}
	return
}
