package soundprint

import (
	"math"
	"testing"
)

func TestAsideOf(t *testing.T) {
	for _, c := range []struct {
		title, album string
		ms           int64
		want         Aside
	}{
		{"Teardrop", "Mezzanine", 330_000, 0},
		{"Intro", "Mezzanine", 120_000, AsideInterlude},
		{"Skit (Phone Call)", "", 200_000, AsideInterlude},
		{"Untitled", "", 40_000, AsideInterlude},
		{"Roads (Live)", "Dummy", 300_000, AsideLive},
		{"Roads", "Roseland NYC Live", 300_000, AsideLive},
		{"Roads", "Live at Roseland", 300_000, AsideLive},
		{"All Apologies", "MTV Unplugged in New York", 230_000, AsideLive},
		{"Live Forever", "Definitely Maybe", 276_000, 0}, // "Live" is the song's name
		{"Blue Monday (Remix)", "", 400_000, AsideRemix},
		{"Strobe (Club Mix)", "", 600_000, AsideRemix},
		{"Strobe (Radio Edit)", "", 200_000, 0},
	} {
		if got := AsideOf(c.title, c.album, c.ms); got != c.want {
			t.Errorf("%q on %q: %b, want %b", c.title, c.album, got, c.want)
		}
	}
	// Asides are left out unless allowed; a seed of one kind allows that kind.
	live := &Track{ItemID: 1, Aside: AsideLive}
	o := Options{}
	if o.allowed(live) {
		t.Error("a live track played without being allowed")
	}
	o.AllowAside = allowAside([]*Track{live})
	if !o.allowed(live) || o.allowed(&Track{ItemID: 2, Aside: AsideRemix}) {
		t.Error("allowAside")
	}
}

func TestDetectLanguage(t *testing.T) {
	en := `[00:12.00] I don't know what you want from me, and I'm tired of the way that you love me
	when the night is over and the morning comes, you are the one that I want, my love, is it you?
	Tell me what you need and I will be there with you, this is the song for you and me`
	es := `No sé qué quieres de mí, pero yo te quiero con todo mi corazón, y cuando la noche se va
	tú eres el amor que me queda, nada es como tú, mi amor, para siempre, porque eres mi vida y mi canción
	y por eso te canto con el alma, que no se acabe nunca esta noche contigo`
	fr := `Je ne sais pas ce que tu veux de moi, mais je t'aime et c'est pour toi que je chante
	dans la nuit les étoiles brillent pour nous, mon amour, avec toi je suis bien, et moi je reste là
	tu es le soleil de mes jours, et je t'attends toujours dans le silence des heures`
	ja := `夜空に輝く星のように、君のことをずっと思っているよ。いつまでも一緒にいたいから、この歌を君に届けたい。
	明日もきっと晴れるはず、だから泣かないで。君の笑顔が見たいんだ、ずっとそばにいてほしい。`
	ru := `Я не знаю, что ты хочешь от меня, но я люблю тебя всем сердцем, и когда ночь уходит,
	ты остаёшься со мной, моя любовь, навсегда, потому что ты моя жизнь и моя песня`
	for lang, text := range map[string]string{"en": en, "es": es, "fr": fr, "ja": ja, "ru": ru} {
		if got := DetectLanguage(text); got != lang {
			t.Errorf("%s lyrics read as %q", lang, got)
		}
	}
	if got := DetectLanguage("la la la oh yeah"); got != "" {
		t.Errorf("too little to say, but read as %q", got)
	}
}

func TestCamelotAndFit(t *testing.T) {
	for _, c := range []struct {
		key, mode string
		n         int
		major     bool
	}{{"C", "major", 8, true}, {"A", "minor", 8, false}, {"G", "major", 9, true}, {"E", "minor", 9, false}, {"F", "major", 7, true}, {"F#", "major", 2, true}} {
		n, major, ok := camelot(c.key, c.mode)
		if !ok || n != c.n || major != c.major {
			t.Errorf("%s %s: %d%v, want %d%v", c.key, c.mode, n, major, c.n, c.major)
		}
	}
	tr := func(key, mode string, bpm float64) *Track { return &Track{Key: key, Mode: mode, BPM: bpm} }
	if f := keyFit(tr("C", "major", 0), tr("A", "minor", 0)); f != 0.8 {
		t.Errorf("relative minor: %v", f)
	}
	if f := keyFit(tr("C", "major", 0), tr("G", "major", 0)); f != 0.8 {
		t.Errorf("a fifth up: %v", f)
	}
	if f := keyFit(tr("C", "major", 0), tr("F#", "major", 0)); f != 0 {
		t.Errorf("a clash: %v", f)
	}
	if f := keyFit(tr("", "", 0), tr("C", "major", 0)); f != 0.5 {
		t.Errorf("unknown: %v", f)
	}
	if f := tempoFit(tr("", "", 120), tr("", "", 60)); f < 0.99 {
		t.Errorf("half time counts as the same: %v", f)
	}
	if f := tempoFit(tr("", "", 120), tr("", "", 150)); f != 0 {
		t.Errorf("25%% apart: %v", f)
	}
}

// A mix ordered along the arc starts calm, peaks about two thirds in and eases off.
func TestArcOrder(t *testing.T) {
	var ts []*Track
	for i := 1; i <= 20; i++ {
		ts = append(ts, &Track{ItemID: int64(i), ArtistID: int64(i), Vec: []float32{1, 0}, Energy: float64(i) / 20})
	}
	out := arcOrder(ts, []float32{1, 0}, true)
	if len(out) != 20 {
		t.Fatalf("%d tracks", len(out))
	}
	energy := func(from, to int) float64 {
		s := 0.0
		for _, t := range out[from:to] {
			s += t.Energy
		}
		return s / float64(to-from)
	}
	start, peak, end := energy(0, 4), energy(11, 15), energy(17, 20)
	if !(start < peak && end < peak) {
		t.Errorf("energy start %.2f, peak %.2f, end %.2f", start, peak, end)
	}
	if math.Abs(arcTarget(0.65)-0.95) > 1e-9 || arcTarget(0) != 0.3 {
		t.Error("arcTarget")
	}
}

// Skipped tracks steer a station away from their sound; played-through ones towards theirs;
// an artist skipped twice is held back.
func TestSteer(t *testing.T) {
	x := NewIndex()
	x.Put(&Track{ItemID: 1, ArtistID: 7, Vec: []float32{1, 0}})
	x.Put(&Track{ItemID: 2, ArtistID: 7, Vec: []float32{0.9, 0.1}})
	x.Put(&Track{ItemID: 3, ArtistID: 8, Vec: []float32{0, 1}})
	x.Put(&Track{ItemID: 4, ArtistID: 7, Vec: []float32{0.8, 0.2}})
	anchor := normalize([]float32{1, 1})
	o := Options{Skipped: []int64{1, 2}, Finished: []int64{3}}
	got := o.steer(x, anchor)
	if dot(got, []float32{0, 1}) <= dot(anchor, []float32{0, 1}) {
		t.Errorf("not steered towards what was played through: %v", got)
	}
	if o.Avoid[4] < steerArtistAvoid || o.Avoid[3] != 0 {
		t.Errorf("artist skipped twice not held back: %v", o.Avoid)
	}
	if same := (&Options{}).steer(x, anchor); dot(same, anchor) < 0.9999 {
		t.Error("steered without skips or plays")
	}
}

func TestLanguagesOf(t *testing.T) {
	var ts []*Track
	for i := 0; i < 18; i++ {
		ts = append(ts, &Track{Language: "en"})
	}
	ts = append(ts, &Track{Language: "es"}, &Track{Language: "es"}, &Track{}) // 2 of 20 in Spanish
	l := LanguagesOf(ts)
	if l["en"] != 1 || l["es"] >= 1 || l["es"] <= 0 {
		t.Errorf("%v", l)
	}
	if p := l.penalty(&Track{Language: "de"}); p != languagePenalty {
		t.Errorf("another language: %v", p)
	}
	if p := l.penalty(&Track{}); p != 0 {
		t.Errorf("instrumental: %v", p)
	}
	if LanguagesOf(ts[:5]) != nil {
		t.Error("too few to say")
	}
}
