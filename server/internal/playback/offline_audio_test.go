package playback

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOfflineAudioArgs(t *testing.T) {
	has := func(args []string, want ...string) bool {
		return strings.Contains(" "+strings.Join(args, " ")+" ", " "+strings.Join(want, " ")+" ")
	}
	// Lossless is converted at the chosen quality, in stereo.
	a := offlineAudioArgs("in.flac", &AudioStream{Index: 0, Codec: "flac", Channels: 6}, 900, 160, "out.m4a")
	if !has(a, "-c:a", "aac", "-b:a", "160k", "-ac", "2") || a[len(a)-1] != "out.m4a" {
		t.Errorf("flac: %v", a)
	}
	// Never above the original's bitrate, and a mono song stays mono.
	a = offlineAudioArgs("in.mp3", &AudioStream{Index: 0, Codec: "mp3", Channels: 1, BitrateKbps: 128}, 130, 256, "o.m4a")
	if !has(a, "-b:a", "128k", "-ac", "1") {
		t.Errorf("mp3: %v", a)
	}
	// AAC at or below the quality is copied: converting it again would only lose quality.
	a = offlineAudioArgs("in.m4a", &AudioStream{Index: 1, Codec: "aac", Channels: 2}, 128, 160, "o.m4a")
	if !has(a, "-map", "0:1") || !has(a, "-c:a", "copy") {
		t.Errorf("aac low: %v", a)
	}
	// AAC above the quality is converted down, and HE-AAC never copied.
	a = offlineAudioArgs("in.m4a", &AudioStream{Index: 0, Codec: "aac", Channels: 2}, 320, 160, "o.m4a")
	if slices.Contains(a, "copy") {
		t.Errorf("aac high: %v", a)
	}
	a = offlineAudioArgs("in.m4a", &AudioStream{Index: 0, Codec: "aac", Profile: "HE-AAC", Channels: 2}, 64, 160, "o.m4a")
	if slices.Contains(a, "copy") {
		t.Errorf("he-aac: %v", a)
	}
}

// The arguments work with a real FFmpeg, where there is one: the result is a smaller AAC M4A
// with the song's tags and no cover-art video stream.
func TestOfflineAudioWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("no ffprobe")
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.flac"), filepath.Join(dir, "out.m4a")
	if b, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-metadata", "title=Tagged", "-ac", "2", in).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	m := &Manager{FFmpeg: ffmpeg}
	var last float64
	args := offlineAudioArgs(in, &AudioStream{Index: 0, Codec: "flac", Channels: 2}, 900, OfflineAudioKbps["low"], out)
	if err := m.runConversion(context.Background(), "aac", args, 3000, func(p float64) { last = p }); err != nil {
		t.Fatal(err)
	}
	if last <= 0 {
		t.Error("no progress reported")
	}
	got, err := exec.Command(probe, "-v", "error", "-show_entries", "stream=codec_name,codec_type:format_tags=title", "-of", "default=nw=1:nk=1", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	if s := string(got); !strings.Contains(s, "aac") || strings.Contains(s, "video") || !strings.Contains(s, "Tagged") {
		t.Errorf("probe: %s", s)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatalf("output: %v %v", st, err)
	}
}

// Naming a rate has AAC small enough to copy encoded instead, which is how surround gets
// downmixed for a speaker; without one a phone's download is copied as ever.
func TestOfflineAudioArgsAtEncodes(t *testing.T) {
	a := offlineAudioArgsAt("in.m4a", &AudioStream{Index: 0, Codec: "aac", Channels: 6}, 128, 256, 48000, "o.m4a")
	got := strings.Join(a, " ")
	if strings.Contains(got, "-c:a copy") || !strings.Contains(got, "-c:a aac") || !strings.Contains(got, "-ac 2") || !strings.Contains(got, "-ar 48000") {
		t.Fatal(got)
	}
	got = strings.Join(offlineAudioArgs("in.m4a", &AudioStream{Index: 0, Codec: "aac", Channels: 6}, 128, 256, "o.m4a"), " ")
	if !strings.Contains(got, "-c:a copy") {
		t.Fatal(got)
	}
}
