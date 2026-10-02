package playback

import "testing"

func TestShiftSubtitles(t *testing.T) {
	vtt := "WEBVTT\n\n00:00:01.000 --> 00:00:02.500\nHi\n\n01:59.900 --> 1:02:00.000 align:start\nBye\n"
	if got := string(ShiftVTT([]byte(vtt), 1500)); got != "WEBVTT\n\n00:00:02.500 --> 00:00:04.000\nHi\n\n00:02:01.400 --> 01:02:01.500 align:start\nBye\n" {
		t.Errorf("later:\n%s", got)
	}
	if got := string(ShiftVTT([]byte(vtt), -1200)); got[:37] != "WEBVTT\n\n00:00:00.000 --> 00:00:01.300" {
		t.Errorf("earlier, clamped at zero:\n%s", got)
	}
	ass := "[Events]\nDialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,Hi, there\n"
	if got := string(ShiftASS([]byte(ass), 250)); got != "[Events]\nDialogue: 0,0:00:01.25,0:00:02.75,Default,,0,0,0,,Hi, there\n" {
		t.Errorf("ass: %s", got)
	}
}
