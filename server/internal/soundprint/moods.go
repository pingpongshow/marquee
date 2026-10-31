package soundprint

import (
	"context"
	"math"
	"sort"
	"strings"
)

// Moods (MUSIC-18). A mood was a single phrase put to CLAP ("chill music"), which knows how
// music sounds far better than how it feels. Each mood is now described several ways, wants
// some traits and avoids others (sad, danceable, sung…), sits in an energy band, may want a
// key or tempo, and has the community tags that mean it. Tracks are scored on all of that;
// the best of them, a couple per artist, give the station its sound, so it's anchored in the
// library's own music rather than in the words.

// Mood is what a mood asks of a track.
type Mood struct {
	Phrases []string
	Tags    []string
	// Traits weighs traits by name: positive wants more of it, negative less.
	Traits map[string]float64
	// Energy is the band of relative energy (0 the calmest of the library, 1 the most
	// intense) the mood sits in; zero is any.
	Energy [2]float64
	Mode   string     // "major", "minor" or "" for either
	BPM    [2]float64 // tempo band; 0 at either end leaves it open
}

// Moods are the moods the apps offer, by lower-case name.
var Moods = map[string]Mood{
	"chill": {
		Phrases: []string{"chill relaxed laid-back music", "calm mellow downtempo music", "soft easygoing chillout music"},
		Tags:    []string{"chill", "chillout", "chill out", "relaxing", "relaxed", "mellow", "laid back", "downtempo", "calm", "lounge"},
		Traits:  map[string]float64{"tender": 0.5, "ambient": 0.3, "angry": -0.6, "exciting": -0.4},
		Energy:  [2]float64{0, 0.55},
	},
	"energetic": {
		Phrases: []string{"energetic upbeat high-energy music", "driving fast exciting music", "powerful uplifting energetic song"},
		Tags:    []string{"energetic", "upbeat", "high energy", "uplifting", "powerful", "energy"},
		Traits:  map[string]float64{"exciting": 0.6, "happy": 0.3, "tender": -0.3, "sad": -0.3},
		Energy:  [2]float64{0.55, 1},
		BPM:     [2]float64{105, 0},
	},
	"focus": {
		Phrases: []string{"instrumental focus music for concentration", "calm instrumental background music", "ambient instrumental music for studying"},
		Tags:    []string{"instrumental", "ambient", "study", "focus", "concentration", "post rock", "minimal", "soundtrack"},
		Traits:  map[string]float64{"vocal": -1, "ambient": 0.4, "angry": -0.5, "dance": -0.2},
		Energy:  [2]float64{0.1, 0.65},
	},
	"melancholy": {
		Phrases: []string{"sad melancholic music", "melancholy heartbreak song", "slow mournful emotional music"},
		Tags:    []string{"sad", "melancholic", "melancholy", "depressing", "heartbreak", "emotional", "sadcore"},
		Traits:  map[string]float64{"sad": 0.9, "tender": 0.2, "happy": -0.6, "dance": -0.4},
		Energy:  [2]float64{0, 0.6},
		Mode:    "minor",
	},
	"party": {
		Phrases: []string{"party dance music", "upbeat club dance hits", "fun party anthem"},
		Tags:    []string{"party", "dance", "club", "fun", "disco", "dancefloor", "dance pop"},
		Traits:  map[string]float64{"dance": 0.9, "happy": 0.4, "exciting": 0.4, "sad": -0.5, "tender": -0.4},
		Energy:  [2]float64{0.55, 1},
		BPM:     [2]float64{100, 0},
	},
	"romantic": {
		Phrases: []string{"romantic love song", "tender romantic ballad", "sensual slow love music"},
		Tags:    []string{"romantic", "love", "love songs", "sensual", "ballad", "sexy", "love song"},
		Traits:  map[string]float64{"tender": 0.8, "vocal": 0.2, "angry": -0.7, "scary": -0.5},
		Energy:  [2]float64{0, 0.65},
	},
	"dreamy": {
		Phrases: []string{"dreamy ethereal atmospheric music", "hazy dream pop", "floating spacey ambient music"},
		Tags:    []string{"dreamy", "ethereal", "atmospheric", "dream pop", "shoegaze", "ambient", "spacey", "dream"},
		Traits:  map[string]float64{"ambient": 0.6, "tender": 0.3, "angry": -0.6},
		Energy:  [2]float64{0, 0.6},
	},
	"aggressive": {
		Phrases: []string{"aggressive intense heavy music", "angry loud heavy song", "hard intense aggressive rock"},
		Tags:    []string{"aggressive", "heavy", "angry", "intense", "hardcore", "metal", "brutal", "heavy metal"},
		Traits:  map[string]float64{"angry": 0.9, "exciting": 0.3, "tender": -0.6, "sad": -0.2},
		Energy:  [2]float64{0.6, 1},
	},
}

// moodFor is a mood by name; one the server doesn't know is described by its name alone.
func moodFor(name string) Mood {
	if m, ok := Moods[strings.ToLower(strings.TrimSpace(name))]; ok {
		return m
	}
	return Mood{Phrases: soundPhrasings(name), Tags: []string{strings.ToLower(name)}}
}

// Weights of a track's fit to a mood.
const (
	// The sound counts least: CLAP knows how music sounds far better than how it feels.
	moodSound  = 0.25 // how close it sounds to the descriptions (by rank in the library)
	moodTraits = 0.45 // the traits it wants
	moodTags   = 0.3  // the community tags that mean it
	// moodEnergy is the penalty per unit of relative energy outside the mood's band.
	moodEnergy = 1.0
	// moodAnchorTracks is how many of the best-fitting tracks set the station's sound;
	// moodAnchorPerArtist how many of them one artist may be.
	moodAnchorTracks    = 25
	moodAnchorPerArtist = 2
	// moodAffinity lifts tracks in Flow by their fit (the best-fitting by this much).
	moodAffinity = 0.35
	// moodOffFit leaves out the tracks that fit least, when enough are left.
	moodOffFit = 0.3
)

// moodFit scores every track for a mood (0–1, by rank among them): the sound against the
// descriptions, the traits, the tags, then the energy band, key and tempo as penalties.
func (s *Service) moodFit(m Mood, text []float32, tracks []*Track) map[int64]float64 {
	sound := map[int64]float64{}
	sims := make([]Scored, len(tracks))
	for i, t := range tracks {
		sims[i] = Scored{t, dot(text, t.Vec)}
	}
	sort.Slice(sims, func(i, j int) bool { return sims[i].Score < sims[j].Score })
	for i, sc := range sims {
		sound[sc.ItemID] = float64(i) / math.Max(1, float64(len(sims)-1))
	}
	ranks := map[string]map[int64]float64{}
	weight := 0.0
	for name, w := range m.Traits {
		ranks[name] = TraitRanks(tracks, name)
		weight += math.Abs(w)
	}
	energy := energyRanks(tracks)
	raw := make([]Scored, len(tracks))
	for i, t := range tracks {
		f := moodSound * sound[t.ItemID]
		if t.Traits != nil && weight > 0 {
			tf := 0.0
			for name, w := range m.Traits {
				r := ranks[name][t.ItemID]
				if w < 0 {
					r = 1 - r
				}
				tf += math.Abs(w) * r
			}
			f += moodTraits * tf / weight
		} else {
			f += moodTraits * 0.5 // no traits yet: neither for nor against
		}
		f += moodTags * s.TagScore(t, m.Tags)
		if m.Energy != [2]float64{} && t.Energy > 0 {
			e := energy[t.ItemID]
			if e < m.Energy[0] {
				f -= moodEnergy * (m.Energy[0] - e)
			} else if e > m.Energy[1] {
				f -= moodEnergy * (e - m.Energy[1])
			}
		}
		if m.Mode != "" && t.Mode != "" && t.Mode != m.Mode {
			f -= 0.05
		}
		if bpm := t.BPM; bpm > 0 {
			if bpm < 80 {
				bpm *= 2 // beat tracking often lands on half time
			}
			if (m.BPM[0] > 0 && bpm < m.BPM[0]) || (m.BPM[1] > 0 && bpm > m.BPM[1]) {
				f -= 0.1
			}
		}
		raw[i] = Scored{t, f}
	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].Score < raw[j].Score })
	out := make(map[int64]float64, len(raw))
	for i, sc := range raw {
		out[sc.ItemID] = float64(i) / math.Max(1, float64(len(raw)-1))
	}
	return out
}

// MoodRadio starts a station for a mood (see Mood): the tracks that fit it best, a couple per
// artist, set its sound; Flow then plays from there, lifting the tracks that fit and leaving
// out those that fit least.
func (s *Service) MoodRadio(ctx context.Context, name string, o Options) (Station, error) {
	m := moodFor(name)
	text, err := s.TextVectorMulti(ctx, m.Phrases)
	if err != nil {
		return Station{}, err
	}
	tracks := s.Index.Where(o.allowed)
	if len(tracks) == 0 {
		return Station{}, ErrNotAnalyzed
	}
	fit := s.moodFit(m, text, tracks)
	best := append([]*Track{}, tracks...)
	sort.SliceStable(best, func(i, j int) bool { return fit[best[i].ItemID] > fit[best[j].ItemID] })
	var anchorTracks []*Track
	perArtist := map[int64]int{}
	for _, t := range best {
		if len(anchorTracks) == moodAnchorTracks {
			break
		}
		if t.ArtistID != 0 && perArtist[t.ArtistID] >= moodAnchorPerArtist {
			continue
		}
		perArtist[t.ArtistID]++
		anchorTracks = append(anchorTracks, t)
	}
	anchor := BlendVectors([][]float32{text, Mean(anchorTracks)}, []float64{0.35, 0.65})
	if o.Affinity == nil {
		o.Affinity = map[int64]float64{}
	}
	for id, f := range fit {
		o.Affinity[id] += moodAffinity * f
	}
	// Off-mood tracks are left out when plenty of others remain.
	if kept := int(float64(len(tracks)) * (1 - moodOffFit)); kept >= max(150, 5*o.Length) {
		inner := o.Keep
		o.Keep = func(t *Track) bool {
			return fit[t.ItemID] >= moodOffFit && (inner == nil || inner(t))
		}
	}
	return Station{Title: upperFirst(strings.TrimSpace(name)) + " Radio", IDs: TrackIDs(s.Index.Flow(anchor, nil, o))}, nil
}

func upperFirst(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}
