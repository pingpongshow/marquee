package vod

import "testing"

// An M3U film linking to a file is made into HLS by the server; only an HLS link passes through.
func TestIsVideoFile(t *testing.T) {
	for link, want := range map[string]bool{
		"http://cdn.example/films/alpha.mp4":               true,
		"http://cdn.example/films/Alpha.MKV?token=abc":     true,
		"http://cdn.example/movie/u/p/42.ts":               true,
		"http://cdn.example/films/alpha/master.m3u8":       false,
		"http://cdn.example/films/alpha/index.m3u8?x=.mp4": false,
		"http://cdn.example/play/42":                       false,
		"::not a link":                                     false,
	} {
		if got := isVideoFile(link); got != want {
			t.Errorf("isVideoFile(%q) = %v, want %v", link, got, want)
		}
	}
}
