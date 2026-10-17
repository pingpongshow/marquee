package encode

import "testing"

func TestDecodeErrorsIgnoresNullMuxer(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"[null @ 0x563b5450b340] Application provided invalid, non monotonically increasing dts to muxer in stream 0: 4 >= 4\n", ""},
		{"[null @ 0x1] dts warning\n[h264 @ 0x2] error while decoding MB 3 4\n", "[h264 @ 0x2] error while decoding MB 3 4"},
		{"[hevc @ 0x2] Invalid NAL unit size\n", "[hevc @ 0x2] Invalid NAL unit size"},
	}
	for _, c := range cases {
		if got := decodeErrors(c.in); got != c.want {
			t.Errorf("decodeErrors(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
