package items

import "testing"

func TestParseAudioFormat(t *testing.T) {
	if a := parseAudioFormat("flac|2116|96000|24|2"); a != (AudioFormat{Codec: "flac", BitrateKbps: 2116, SampleRate: 96000, BitDepth: 24, Chans: 2}) {
		t.Errorf("parsed %+v", a)
	}
	if a := parseAudioFormat(""); a != (AudioFormat{}) {
		t.Errorf("empty: %+v", a)
	}
}
