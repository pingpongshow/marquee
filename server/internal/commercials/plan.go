package commercials

import "sort"

// Segment is a stretch of the recording that's kept, in milliseconds from its start.
type Segment struct {
	StartMS, EndMS int64
}

// Plan is how a recording is cut: the stretches kept, what's removed, and whether stream
// copy cuts it cleanly.
type Plan struct {
	Keep    []Segment
	Removed []Break
	// Copy is false when stream copy can't cut cleanly: a break shorter than the gap between
	// keyframes, or more than maxAdTail of advert left in front of the programme.
	Copy bool
}

// maxAdTail is how much advert stream copy may leave before the programme resumes (the
// distance from the last keyframe before a break's end to that end).
const maxAdTail = 5_000

// RemovedMS is the total time cut out.
func (p Plan) RemovedMS() int64 {
	var n int64
	for _, b := range p.Removed {
		n += b.EndMS - b.StartMS
	}
	return n
}

// KeptMS is the length of the cut recording.
func (p Plan) KeptMS() int64 {
	var n int64
	for _, k := range p.Keep {
		n += k.EndMS - k.StartMS
	}
	return n
}

// MakePlan works out the cuts for breaks in a recording durationMS long with video
// keyframes at keyframes (ms, sorted). With stream copy a kept stretch must start on a
// keyframe; it starts on the last keyframe at or before the break's end, so none of the
// programme is lost (a little advert may stay) — never on one after it, which would drop
// programme frames. It may end anywhere: the programme runs right up to the break. When
// copy can't cut cleanly (see Plan.Copy), the plan still holds the exact cuts, for a
// re-encode.
func MakePlan(breaks []Break, keyframes []int64, durationMS int64) Plan {
	p := Plan{Copy: len(keyframes) > 0}
	pos := int64(0)
	for _, b := range breaks {
		if b.StartMS <= pos || b.StartMS >= durationMS {
			continue
		}
		end := min(b.EndMS, durationMS)
		resume := end
		if kf, ok := keyframeAtOrBefore(keyframes, end); ok && kf > b.StartMS && end-kf <= maxAdTail {
			resume = kf
		} else {
			p.Copy = false
		}
		if resume <= b.StartMS {
			continue
		}
		p.Keep = append(p.Keep, Segment{pos, b.StartMS})
		p.Removed = append(p.Removed, Break{b.StartMS, resume})
		pos = resume
	}
	if pos < durationMS {
		p.Keep = append(p.Keep, Segment{pos, durationMS})
	}
	if !p.Copy {
		// Exact cuts for a re-encode.
		return exactPlan(breaks, durationMS)
	}
	return p
}

func exactPlan(breaks []Break, durationMS int64) Plan {
	p := Plan{}
	pos := int64(0)
	for _, b := range breaks {
		if b.StartMS <= pos || b.StartMS >= durationMS {
			continue
		}
		end := min(b.EndMS, durationMS)
		p.Keep = append(p.Keep, Segment{pos, b.StartMS})
		p.Removed = append(p.Removed, Break{b.StartMS, end})
		pos = end
	}
	if pos < durationMS {
		p.Keep = append(p.Keep, Segment{pos, durationMS})
	}
	return p
}

func keyframeAtOrBefore(kfs []int64, t int64) (int64, bool) {
	i := sort.Search(len(kfs), func(i int) bool { return kfs[i] > t })
	if i == 0 {
		return 0, false
	}
	return kfs[i-1], true
}

// CutPosition maps a position in the original recording to the cut one (watch positions):
// time removed before it is taken off, and a position inside a removed stretch moves to
// where the programme resumes.
func CutPosition(ms int64, removed []Break) int64 {
	out := ms
	for _, r := range removed {
		switch {
		case ms >= r.EndMS:
			out -= r.EndMS - r.StartMS
		case ms > r.StartMS:
			out -= ms - r.StartMS
		}
	}
	return max(out, 0)
}

// OriginalPosition maps a position in the cut recording back to the original.
func OriginalPosition(ms int64, removed []Break) int64 {
	out := ms
	for _, r := range removed { // sorted
		if out >= r.StartMS {
			out += r.EndMS - r.StartMS
		}
	}
	return out
}
