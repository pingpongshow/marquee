package soundprint

import (
	"math"
	"sort"
)

// How one track leads into the next (MUSIC-17): beyond sounding alike, a mix flows when the
// keys fit (the Camelot wheel DJs mix by) and the tempo doesn't lurch, and a longer one has a
// shape: it starts gently, builds to a peak about two thirds in, and eases off.

var noteIndex = map[string]int{"C": 0, "C#": 1, "D": 2, "D#": 3, "E": 4, "F": 5, "F#": 6, "G": 7, "G#": 8, "A": 9, "A#": 10, "B": 11}

// camelot places a key on the Camelot wheel: its number (1–12, neighbours a fifth apart) and
// whether it's major (B) or minor (A). ok is false for an unknown key.
func camelot(key, mode string) (n int, major, ok bool) {
	i, found := noteIndex[key]
	if !found || (mode != "major" && mode != "minor") {
		return 0, false, false
	}
	major = mode == "major"
	if !major {
		i = (i + 3) % 12 // a minor key sits with its relative major
	}
	// C major is 8B; each step of a fifth (7 semitones) is one number on.
	return (8+(i*7)%12-1)%12 + 1, major, true
}

// keyFit is how well b's key follows a's: 1 the same, 0.8 a neighbour on the wheel or the
// relative major or minor, 0 a clash, 0.5 when either key is unknown.
func keyFit(a, b *Track) float64 {
	na, ma, oka := camelot(a.Key, a.Mode)
	nb, mb, okb := camelot(b.Key, b.Mode)
	if !oka || !okb {
		return 0.5
	}
	d := (na - nb + 12) % 12
	switch {
	case d == 0 && ma == mb:
		return 1
	case d == 0, (d == 1 || d == 11) && ma == mb:
		return 0.8
	}
	return 0
}

// tempoFit is how close the tempos are (1 the same, 0 at 15% apart or more), half and double
// time counting as the same (beat tracking often lands an octave off); 0.5 when unknown.
func tempoFit(a, b *Track) float64 {
	if a.BPM <= 0 || b.BPM <= 0 {
		return 0.5
	}
	d := math.Abs(math.Log(b.BPM / a.BPM))
	d = math.Min(d, math.Abs(d-math.Ln2))
	return math.Max(0, 1-d/math.Log(1.15))
}

// transition is how well b follows a by key and tempo (0–1, 0.5 neutral).
func transition(a, b *Track) float64 {
	return 0.5*keyFit(a, b) + 0.5*tempoFit(a, b)
}

// arcTarget is where in the list's energy range position p (0 the first track, 1 the last)
// should sit: from a third of the way up to the top at about two thirds in, then down to
// the middle.
func arcTarget(p float64) float64 {
	if p < 0.65 {
		return 0.3 + 0.65*p/0.65
	}
	return 0.95 - 0.45*(p-0.65)/0.35
}

// energyRanks ranks the tracks by energy among themselves (0 the calmest, 1 the most
// intense); tracks without an energy reading sit in the middle.
func energyRanks(ts []*Track) map[int64]float64 {
	var with []*Track
	for _, t := range ts {
		if t.Energy > 0 {
			with = append(with, t)
		}
	}
	out := make(map[int64]float64, len(ts))
	for _, t := range ts {
		out[t.ItemID] = 0.5
	}
	sort.Slice(with, func(i, j int) bool { return with[i].Energy < with[j].Energy })
	for i, t := range with {
		if len(with) > 1 {
			out[t.ItemID] = float64(i) / float64(len(with)-1)
		}
	}
	return out
}

// Weights of the ordering: the energy arc, and key and tempo against plain sound.
const (
	arcWeight        = 0.25
	transitionWeight = 0.1
)

// arcOrder orders a mix along the energy arc: each next track is close in sound to the one
// before it, follows it well in key and tempo, and sits near the arc's energy for its place;
// with apart, an artist doesn't play twice in a row (as far as the artists allow). The first
// track is the best match for v among the calmer ones.
func arcOrder(ts []*Track, v []float32, apart bool) []*Track {
	if len(ts) < 3 {
		return ts
	}
	ranks := energyRanks(ts)
	var out []*Track
	left := append([]*Track{}, ts...)
	ref := v
	for len(left) > 0 {
		var cnt map[int64]int
		if apart {
			cnt = map[int64]int{}
			for _, t := range left {
				if t.ArtistID != 0 {
					cnt[t.ArtistID]++
				}
			}
		}
		target := arcTarget(float64(len(out)) / float64(len(ts)-1))
		best, bestScore, fit, fitScore := -1, math.Inf(-1), -1, math.Inf(-1)
		for i, t := range left {
			s := dot(ref, t.Vec) - arcWeight*math.Abs(ranks[t.ItemID]-target)
			if len(out) > 0 {
				s += transitionWeight * (transition(out[len(out)-1], t) - 0.5)
			}
			if s > bestScore {
				best, bestScore = i, s
			}
			if apart && (len(out) == 0 || t.ArtistID != out[len(out)-1].ArtistID || t.ArtistID == 0) &&
				spaced(cnt, len(left)-1, t.ArtistID) && s > fitScore {
				fit, fitScore = i, s
			}
		}
		if fit >= 0 {
			best = fit
		}
		out = append(out, left[best])
		ref = left[best].Vec
		left = append(left[:best], left[best+1:]...)
	}
	return out
}

// Languages weighs the languages a list may be sung in (0–1 each); a track in another
// language counts against it, an instrumental or unknown one doesn't. Nil allows any.
type Languages map[string]float64

// languagePenalty is how much a track sung in a language outside the list's counts against it.
const languagePenalty = 0.2

func (l Languages) penalty(t *Track) float64 {
	if len(l) == 0 || t.Language == "" {
		return 0
	}
	return languagePenalty * (1 - l[t.Language])
}

// LanguagesOf is the languages tracks are sung in, by share (those under 15% of the tracks
// with a language count for little), nil when too few say: a list from them keeps to those
// languages unless they're mixed.
func LanguagesOf(ts []*Track) Languages {
	count := map[string]int{}
	n := 0
	for _, t := range ts {
		if t.Language != "" {
			count[t.Language]++
			n++
		}
	}
	if n < 8 {
		return nil
	}
	out := Languages{}
	for lang, c := range count {
		share := float64(c) / float64(n)
		if share >= 0.15 {
			out[lang] = 1
		} else {
			out[lang] = share / 0.15
		}
	}
	return out
}
