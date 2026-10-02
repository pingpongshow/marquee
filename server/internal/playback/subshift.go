package playback

import (
	"fmt"
	"regexp"
	"strconv"
)

// Subtitle timing offsets (PLAY-17): cue times are moved by offset ms (positive = later).
// Cues pushed before zero start at zero.

var (
	vttTime = regexp.MustCompile(`(?m)((?:\d+:)?\d{2}:\d{2}\.\d{3})( --> )((?:\d+:)?\d{2}:\d{2}\.\d{3})`)
	assTime = regexp.MustCompile(`(?m)^(Dialogue:\s*[^,]*,)(\d+:\d{2}:\d{2}\.\d{2}),(\d+:\d{2}:\d{2}\.\d{2})`)
)

// ShiftVTT moves every WebVTT cue by offset ms.
func ShiftVTT(data []byte, offset int) []byte {
	if offset == 0 {
		return data
	}
	return vttTime.ReplaceAllFunc(data, func(m []byte) []byte {
		p := vttTime.FindSubmatch(m)
		return []byte(fmtVTT(parseClock(string(p[1]))+offset) + string(p[2]) + fmtVTT(parseClock(string(p[3]))+offset))
	})
}

// ShiftASS moves every ASS dialogue line by offset ms.
func ShiftASS(data []byte, offset int) []byte {
	if offset == 0 {
		return data
	}
	return assTime.ReplaceAllFunc(data, func(m []byte) []byte {
		p := assTime.FindSubmatch(m)
		return []byte(string(p[1]) + fmtASS(parseClock(string(p[2]))+offset) + "," + fmtASS(parseClock(string(p[3]))+offset))
	})
}

// parseClock reads [h:]mm:ss.fff (or .ff) as ms.
func parseClock(s string) int {
	var parts []int
	frac := 0
	cur := ""
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case ':':
			n, _ := strconv.Atoi(cur)
			parts, cur = append(parts, n), ""
		case '.':
			n, _ := strconv.Atoi(cur)
			parts, cur = append(parts, n), ""
			f := s[i+1:]
			frac, _ = strconv.Atoi(f)
			for l := len(f); l < 3; l++ {
				frac *= 10
			}
			i = len(s)
		default:
			cur += string(c)
		}
	}
	ms := frac
	mult := 1000
	for i := len(parts) - 1; i >= 0; i-- {
		ms += parts[i] * mult
		mult *= 60
	}
	return ms
}

func fmtVTT(ms int) string {
	ms = max(ms, 0)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func fmtASS(ms int) string {
	ms = max(ms, 0)
	return fmt.Sprintf("%d:%02d:%02d.%02d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000/10)
}
