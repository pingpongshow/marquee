package playback

import "testing"

type tsub struct{ lang, codec string }

// An English PGS track listed before an English SRT: the SRT is chosen, so the video
// needn't be transcoded to burn the picture subtitles in.
func TestAutoSubtitlePrefersText(t *testing.T) {
	subs := []tsub{{"eng", "hdmv_pgs_subtitle"}, {"fre", "hdmv_pgs_subtitle"}, {"eng", "subrip"}}
	audio := tsub{"jpn", "eac3"}
	info := func(s *tsub) (string, bool) { return s.lang, false }
	r := Request{UserSubMode: "foreign", UserAudioLang: "eng"}
	got := autoSubtitle(textFirst(subs, func(s tsub) string { return s.codec }), r, &audio, info)
	if got == nil || got.codec != "subrip" {
		t.Fatalf("picked %+v", got)
	}
	// Only picture subtitles: still picked (burned in).
	got = autoSubtitle(textFirst(subs[:2], func(s tsub) string { return s.codec }), r, &audio, info)
	if got == nil || got.codec != "hdmv_pgs_subtitle" || got.lang != "eng" {
		t.Fatalf("picked %+v", got)
	}
}
