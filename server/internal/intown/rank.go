package intown

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"marquee/internal/items"
)

// Ratings are 0–10, half stars.
const (
	highRating = 7.0 // 3.5 stars
	lowRating  = 4.0 // 2 stars: below this is a clear dislike
	// likedPlays is how many plays make an artist one they like; one could be an accident.
	likedPlays = 2
	// loudPlays is when "you've played them" becomes "you play them a lot".
	loudPlays = 20
)

// Reason kinds, as in the API.
const (
	ReasonFavourite  = "favourite"
	ReasonRated      = "rated"
	ReasonPlayed     = "played"
	ReasonSimilar    = "similar"
	ReasonSoundsLike = "soundsLike"
	ReasonLibrary    = "library"
	ReasonGenre      = "genre"
	ReasonNearby     = "nearby"
)

// dislikes reports an artist someone clearly doesn't like: their rating is below 2 stars (or,
// with no rating of the artist, their ratings of the music average below that), or they gave
// most of the songs they've marked or played a thumbs down. A heart overrides it all.
func dislikes(l items.ArtistLiking) bool {
	if l.Favorite {
		return false
	}
	if l.Rating > 0 {
		if l.Rating < lowRating {
			return true
		}
	} else if l.RatedParts >= 2 && l.RatedSum/float64(l.RatedParts) < lowRating {
		return true
	}
	if l.Thumbs == 0 || l.Thumbs*2 <= l.Touched {
		return false
	}
	return l.Thumbs >= 2 || l.Thumbs == l.Songs
}

// likingScore is how much someone likes an artist: a heart counts most, then a high rating,
// then favourites and high ratings among their music, then plays. 0 says nothing.
func likingScore(l items.ArtistLiking) float64 {
	v := 0.0
	if l.Favorite {
		v += 100
	}
	if l.Rating >= highRating {
		v += 40 + (l.Rating-highRating)*10
	}
	if l.FavoriteParts > 0 {
		v += 30 + 2*float64(min(l.FavoriteParts, 10))
	}
	if l.HighRated > 0 {
		v += 20 + float64(min(l.HighRated, 10))
	}
	if l.Plays >= likedPlays {
		v += min(20, 5*math.Log2(1+float64(l.Plays)))
	}
	return v
}

// likedReason says why an artist counts as liked, in a few words.
func likedReason(l items.ArtistLiking) (kind, text string) {
	switch {
	case l.Favorite:
		return ReasonFavourite, "One of your favourites"
	case l.Rating >= highRating:
		return ReasonRated, "You rated them highly"
	case l.FavoriteParts > 0:
		return ReasonFavourite, "You've favourited their music"
	case l.HighRated > 0:
		return ReasonRated, "You rated their music highly"
	case l.Plays >= loudPlays:
		return ReasonPlayed, "You play them a lot"
	case l.Plays >= likedPlays:
		return ReasonPlayed, "You've played them"
	}
	return "", ""
}

func mergeLiking(a, b items.ArtistLiking) items.ArtistLiking {
	a.Favorite = a.Favorite || b.Favorite
	a.Rating = max(a.Rating, b.Rating)
	a.FavoriteParts += b.FavoriteParts
	a.HighRated += b.HighRated
	a.RatedParts += b.RatedParts
	a.RatedSum += b.RatedSum
	a.Plays += b.Plays
	a.Thumbs += b.Thumbs
	a.Touched += b.Touched
	a.Songs += b.Songs
	a.Genres = append(a.Genres, b.Genres...)
	return a
}

// genreKey makes genres comparable: "Hip-Hop/Rap" and "hip hop rap" are one.
func genreKey(g string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(g, "&", " and ")), func(r rune) bool {
		return r == ' ' || r == '-' || r == '/' || r == '_'
	}), " ")
}

// topGenres is someone's most liked genres, weighted by how much they like the artists in them.
func topGenres(artists map[int64]items.ArtistLiking, n int) []string {
	weights := map[string]float64{}
	for _, l := range artists {
		score := likingScore(l)
		if score == 0 || dislikes(l) {
			continue
		}
		seen := map[string]bool{}
		for _, g := range l.Genres {
			if k := genreKey(g); k != "" && !seen[k] {
				seen[k] = true
				weights[k] += score
			}
		}
	}
	keys := make([]string, 0, len(weights))
	for k := range weights {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(weights[b], weights[a]), cmp.Compare(a, b))
	})
	return keys[:min(n, len(keys))]
}

// genreMatch finds the source's genre among someone's top ones. One contains the other only as
// whole words: "alternative rock" is rock, "rockabilly" isn't.
func genreMatch(show Show, top []string) (string, int) {
	for _, g := range []string{show.SubGenre, show.Genre} {
		if g == "" {
			continue
		}
		k := genreKey(g)
		for rank, t := range top {
			if t == k || wholeWords(t, k) || wholeWords(k, t) {
				return g, rank
			}
		}
	}
	return "", 0
}

func wholeWords(s, sub string) bool {
	return strings.Contains(" "+s+" ", " "+sub+" ")
}

// Taste is what ranking goes by, all of it already limited to what the person may see.
type Taste struct {
	// Liking is by artist (one entry for an artist in two libraries, under its Rep).
	Liking map[int64]items.ArtistLiking
	Names  map[int64]string
	// Similar is what listeners consider alike: a liked artist to others, 0–1.
	Similar func(artist int64) map[int64]float64
	// SoundsLike is artists that sound like a liked one: the artist to the liked artist.
	SoundsLike map[int64]int64
	// Genres are their top genres, best first.
	Genres []string
}

// actRef is an act on a show's bill and, when it's in a library the person can see, its artist.
type actRef struct {
	Act    Attraction
	ItemID int64
}

// nearCandidate is a show to rank, with its bill worked out.
type nearCandidate struct {
	View ShowView
	Acts []actRef
}

// Near list limits. A bill can be on for several nights, and a list isn't helped by all of them.
const (
	maxNearShows   = 60
	maxNearPerAct  = 4
	maxNearNearby  = 20
	maxLikedForSim = 30
)

// Tiers of the near list: artists they like lead.
const (
	tierLiked = iota
	tierSimilar
	tierSoundsLike
	tierLibrary
	tierGenre
	tierNearby
)

// ranked is a show with its place in the list.
type ranked struct {
	ShowView
	tier  int
	score float64
}

// rankNear orders shows near someone and gives each a reason. A show whose headliner they
// clearly dislike is left out; a disliked support act is only passed over.
func rankNear(list []nearCandidate, t Taste) []ShowView {
	similarTo := similarIndex(t)
	out := make([]ranked, 0, len(list))
	for _, c := range list {
		if len(c.Acts) > 0 && c.Acts[0].ItemID != 0 && dislikes(t.Liking[c.Acts[0].ItemID]) {
			continue
		}
		out = append(out, bestFor(c, t, similarTo))
	}
	slices.SortStableFunc(out, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(a.tier, b.tier), cmp.Compare(b.score, a.score),
			cmp.Compare(a.LocalDate, b.LocalDate), cmp.Compare(a.LocalTime, b.LocalTime), cmp.Compare(a.ID, b.ID))
	})
	views := make([]ShowView, 0, min(len(out), maxNearShows))
	perAct := map[string]int{}
	nearby := 0
	for _, r := range out {
		if len(views) == maxNearShows {
			break
		}
		if perAct[r.Act] == maxNearPerAct {
			continue
		}
		if r.tier == tierNearby {
			if nearby == maxNearNearby {
				continue
			}
			nearby++
		}
		perAct[r.Act]++
		views = append(views, r.ShowView)
	}
	return views
}

// similarIndex finds, for each artist, the liked artist most like it, weighing how much they're
// liked with how alike the two are.
type similarHit struct {
	liked int64
	score float64
}

func similarIndex(t Taste) map[int64]similarHit {
	out := map[int64]similarHit{}
	if t.Similar == nil {
		return out
	}
	liked := make([]int64, 0, len(t.Liking))
	for id, l := range t.Liking {
		if likingScore(l) > 0 && !dislikes(l) {
			liked = append(liked, id)
		}
	}
	slices.SortFunc(liked, func(a, b int64) int {
		return cmp.Or(cmp.Compare(likingScore(t.Liking[b]), likingScore(t.Liking[a])), cmp.Compare(a, b))
	})
	for _, id := range liked[:min(maxLikedForSim, len(liked))] {
		weight := likingScore(t.Liking[id])
		for other, sim := range t.Similar(id) {
			if other == id || t.Names[other] == "" {
				continue
			}
			if v := sim * weight; v > out[other].score {
				out[other] = similarHit{liked: id, score: v}
			}
		}
	}
	return out
}

// bestFor picks the act on the bill that says most for the show, and the reason.
func bestFor(c nearCandidate, t Taste, similarTo map[int64]similarHit) ranked {
	r := ranked{ShowView: c.View, tier: tierNearby}
	r.Reason, r.ReasonKind = "Near you", ReasonNearby
	pick := func(tier int, score float64, ref actRef, kind, text string) {
		if tier < r.tier || (tier == r.tier && score > r.score) {
			r.tier, r.score = tier, score
			r.ReasonKind, r.Reason = kind, text
			r.Act, r.ItemID = ref.Act.Name, ref.ItemID
		}
	}
	for _, ref := range c.Acts {
		if ref.ItemID == 0 {
			continue
		}
		l := t.Liking[ref.ItemID]
		if dislikes(l) {
			continue
		}
		if score := likingScore(l); score > 0 {
			kind, text := likedReason(l)
			if kind != "" {
				pick(tierLiked, score, ref, kind, text)
				continue
			}
		}
		if hit, ok := similarTo[ref.ItemID]; ok {
			pick(tierSimilar, hit.score, ref, ReasonSimilar, "Like "+t.Names[hit.liked])
			continue
		}
		if liked, ok := t.SoundsLike[ref.ItemID]; ok && t.Names[liked] != "" {
			pick(tierSoundsLike, likingScore(t.Liking[liked]), ref, ReasonSoundsLike, "Sounds like "+t.Names[liked])
			continue
		}
		pick(tierLibrary, 0, ref, ReasonLibrary, "In your library")
	}
	if r.tier > tierGenre {
		if g, rank := genreMatch(c.View.Show, t.Genres); g != "" {
			r.tier, r.score = tierGenre, float64(len(t.Genres)-rank)
			r.ReasonKind, r.Reason = ReasonGenre, "You listen to a lot of "+g
		}
	}
	r.Support = false
	for i, ref := range c.Acts {
		if ref.Act.Name == r.Act {
			r.Support = i > 0
			break
		}
	}
	return r
}
