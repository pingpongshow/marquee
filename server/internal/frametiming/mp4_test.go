package frametiming

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// breakTiming writes src to dst without its composition offsets, the way the broken muxer
// left Sunshine (2007): every frame shown at its decode time. The media is copied as it is.
func breakTiming(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	tops, err := topBoxes(in)
	if err != nil {
		t.Fatal(err)
	}
	var mv topBox
	for _, tb := range tops {
		if tb.typ == "moov" {
			mv = tb
		}
	}
	raw := make([]byte, mv.size)
	in.ReadAt(raw, mv.off)
	parsed, err := parseBoxes(raw)
	if err != nil {
		t.Fatal(err)
	}
	moov := parsed[0]
	stbl := videoTrack(moov).path("mdia", "minf", "stbl")
	if stbl.child("ctts") == nil {
		t.Fatal("the test file has no composition offsets to remove")
	}
	stbl.remove("ctts")
	tables, _ := chunkTables(moov)
	moveChunks(tables, int64(moov.size())-mv.size, mv.off+mv.size)
	var buf bytes.Buffer
	moov.write(&buf)
	whole, _ := os.ReadFile(src)
	out := append(append(append([]byte{}, whole[:mv.off]...), buf.Bytes()...), whole[mv.off+mv.size:]...)
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func ffprobeLines(t *testing.T, args ...string) []string {
	t.Helper()
	out, err := exec.Command("ffprobe", append([]string{"-v", "error"}, args...)...).Output()
	if err != nil {
		t.Fatalf("ffprobe %v: %v", args, err)
	}
	return strings.Fields(string(out))
}

// framesShown is the video's frame times as they're shown (in seconds, from the start).
func framesShown(t *testing.T, path string) []string {
	return ffprobeLines(t, "-select_streams", "v:0", "-show_entries", "frame=pts_time", "-of", "csv=p=0", path)
}

// packetHashes is every packet's checksum, in file order: the media itself.
func packetHashes(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0", "-c", "copy", "-f", "framemd5", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	var hashes []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Split(line, ","); len(f) == 6 && !strings.HasPrefix(line, "#") {
			hashes = append(hashes, strings.TrimSpace(f[0])+":"+strings.TrimSpace(f[4])+":"+strings.TrimSpace(f[5]))
		}
	}
	return strings.Join(hashes, "\n")
}

// A file whose frames are timed in decode order is found, and the repair gives back the
// original's timing exactly, with every packet of audio and video as it was: for HEVC and
// H.264, with the index before the media (moved) and after it.
func TestRepairRestoresTiming(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	for _, c := range []struct {
		name  string
		codec []string
		fast  bool
	}{
		{"hevc-faststart", []string{"-c:v", "libx265", "-x265-params", "log-level=error:bframes=4:keyint=48", "-tag:v", "hvc1"}, true},
		{"hevc-index-last", []string{"-c:v", "libx265", "-x265-params", "log-level=error:bframes=4:keyint=48", "-tag:v", "hvc1"}, false},
		{"h264-faststart", []string{"-c:v", "libx264", "-bf", "3", "-g", "48"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			good, broken, fixed := filepath.Join(dir, "good.mp4"), filepath.Join(dir, "broken.mp4"), filepath.Join(dir, "fixed.mp4")
			args := []string{"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24000/1001:duration=6",
				"-f", "lavfi", "-i", "sine=frequency=440:duration=6"}
			args = append(args, c.codec...)
			args = append(args, "-c:a", "aac", "-b:a", "64k")
			if c.fast {
				args = append(args, "-movflags", "+faststart")
			}
			if out, err := exec.Command("ffmpeg", append(args, good)...).CombinedOutput(); err != nil {
				t.Fatalf("making the test file: %v %s", err, out)
			}
			breakTiming(t, good, broken)

			if reason, err := Check(ctx, nil, "", good); err != nil || reason != "" {
				t.Fatalf("the good file is flagged: %q %v", reason, err)
			}
			reason, err := Check(ctx, nil, "", broken)
			if err != nil || !strings.HasPrefix(reason, "Frames are timed in decode order") {
				t.Fatalf("the broken file isn't flagged: %q %v", reason, err)
			}

			var last float64
			order, err := Order(ctx, nil, "", broken, func(f float64) { last = f })
			if err != nil {
				t.Fatal(err)
			}
			if last > 1 {
				t.Errorf("progress %v", last)
			}
			if _, err := Rewrite(broken, fixed, order); err != nil {
				t.Fatal(err)
			}
			if reason, err := Check(ctx, nil, "", fixed); err != nil || reason != "" {
				t.Fatalf("the repaired file is still flagged: %q %v", reason, err)
			}
			if want, got := framesShown(t, good), framesShown(t, fixed); strings.Join(want, " ") != strings.Join(got, " ") {
				t.Errorf("frames shown at different times than the original's:\n%v\n%v", want[:min(12, len(want))], got[:min(12, len(got))])
			}
			if packetHashes(t, broken) != packetHashes(t, fixed) {
				t.Error("the repair changed the media")
			}
			// Nothing written over an existing file.
			if _, err := Rewrite(broken, fixed, order); err == nil {
				t.Error("rewrote over an existing file")
			}
		})
	}
}

// The offsets show each frame at its decode slot plus the smallest delay; a mismatched
// order is refused.
func TestOffsets(t *testing.T) {
	dts := []uint64{0, 10, 20, 30, 40}
	// Decode order I P B B P' shown as I B B P P': display index → decode index.
	ctts, delay, err := offsets(dts, []int{0, 2, 3, 1, 4})
	if err != nil {
		t.Fatal(err)
	}
	if delay != 10 || ctts[0] != 10 || ctts[1] != 30 || ctts[2] != 0 || ctts[3] != 0 || ctts[4] != 10 {
		t.Errorf("ctts %v delay %d", ctts, delay)
	}
	if _, _, err := offsets(dts, []int{0, 1, 1, 3, 4}); err == nil {
		t.Error("a frame shown twice was accepted")
	}
	if _, _, err := offsets(dts, []int{0, 1, 2}); err == nil {
		t.Error("too few frames were accepted")
	}
	b := cttsBox(ctts)
	if n := binary.BigEndian.Uint32(b.data[4:]); n != 4 {
		t.Errorf("%d entries, want the two zeros merged into 4", n)
	}
}

// Frames the decoder couldn't read keep their place: those decoded around them stay in order,
// and every sample ends up with a time.
func TestPlaceUndecoded(t *testing.T) {
	for _, c := range []struct {
		order []int
		n     int
		want  []int
	}{
		{[]int{0, 2, 3, 1, 4}, 5, []int{0, 2, 3, 1, 4}},          // nothing missing
		{[]int{2, 3, 1, 4}, 5, []int{0, 2, 3, 1, 4}},             // the first
		{[]int{0, 2, 3, 1}, 5, []int{0, 2, 3, 1, 4}},             // the last
		{[]int{0, 3, 1, 4}, 5, []int{0, 3, 2, 1, 4}},             // among reordered frames
		{[]int{0, 4}, 5, []int{0, 1, 2, 3, 4}},                   // a run
		{[]int{0, 5, 3, 4, 7}, 8, []int{0, 1, 2, 5, 3, 4, 6, 7}}, // several apart
	} {
		got := placeUndecoded(c.order, c.n)
		if !slices.Equal(got, c.want) {
			t.Errorf("placeUndecoded(%v, %d) = %v, want %v", c.order, c.n, got, c.want)
		}
	}
}

// A few frames blanked out in the file don't stop the order being worked out, and the repair
// still reads as in order; a lot of them do.
func TestRepairWithDamagedFrames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	good, broken := filepath.Join(dir, "good.mp4"), filepath.Join(dir, "broken.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=10",
		"-c:v", "libx265", "-x265-params", "log-level=error:bframes=4:keyint=48", "-tag:v", "hvc1", good).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	breakTiming(t, good, broken)
	whole, err := Order(ctx, nil, "", broken, nil)
	if err != nil {
		t.Fatal(err)
	}
	packets := ffprobeLines(t, "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pos,size", "-of", "csv=p=0", broken)
	blank := func(path string, samples ...int) {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, i := range samples {
			size, pos, _ := strings.Cut(strings.TrimSuffix(packets[i], ","), ",")
			n, _ := strconv.Atoi(size)
			at, _ := strconv.ParseInt(pos, 10, 64)
			if _, err := f.WriteAt(make([]byte, n), at); err != nil {
				t.Fatal(err)
			}
		}
	}

	damaged, fixed := filepath.Join(dir, "damaged.mp4"), filepath.Join(dir, "fixed.mp4")
	raw, _ := os.ReadFile(broken)
	os.WriteFile(damaged, raw, 0o644)
	blank(damaged, 100)
	order, err := Order(ctx, nil, "", damaged, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != len(whole) {
		t.Fatalf("%d frames placed, want %d", len(order), len(whole))
	}
	skip := func(o []int) []int { return slices.DeleteFunc(slices.Clone(o), func(i int) bool { return i == 100 }) }
	if !slices.Equal(skip(order), skip(whole)) {
		t.Error("the frames around the damage changed order")
	}
	if _, err := Rewrite(damaged, fixed, order); err != nil {
		t.Fatal(err)
	}
	if reason, err := Check(ctx, nil, "", fixed); err != nil || reason != "" {
		t.Errorf("the repaired file is still flagged: %q %v", reason, err)
	}

	os.WriteFile(damaged, raw, 0o644)
	blank(damaged, 60, 70, 80, 90, 100)
	if _, err := Order(ctx, nil, "", damaged, nil); err == nil || !strings.Contains(err.Error(), "frames decoded") {
		t.Errorf("too much damage: %v", err)
	}
}

// A stray byte after the last box doesn't stop a repair, and is kept; without the index or the
// media the file still isn't one to touch.
func TestStrayBytesAfterTheLastBox(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) *os.File {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	box := func(typ string) []byte { return append([]byte{0, 0, 0, 8}, typ...) }
	whole := append(box("mdat"), box("moov")...)
	if tops, err := topBoxes(write("stray.mp4", append(slices.Clone(whole), 0))); err != nil || len(tops) != 2 {
		t.Errorf("a stray byte: %v %v", tops, err)
	}
	if tops, err := topBoxes(write("junk.mp4", append(slices.Clone(whole), 0, 0, 0, 200, 'j', 'u', 'n', 'k'))); err != nil || len(tops) != 2 {
		t.Errorf("a box that overruns the file, after the index and media: %v %v", tops, err)
	}
	if _, err := topBoxes(write("no-index.mp4", append(box("mdat"), 0))); !errors.Is(err, ErrNotMP4) {
		t.Errorf("no index: %v", err)
	}
	if _, err := topBoxes(write("cut.mp4", append(box("moov"), 0, 0, 0, 200, 'm', 'd', 'a', 't'))); !errors.Is(err, ErrNotMP4) {
		t.Errorf("media cut short: %v", err)
	}
}

func TestVerdict(t *testing.T) {
	if r, _ := Verdict(OutOfOrder([]int64{0, 1, 2, 3})); r != "" {
		t.Errorf("rising times flagged: %q", r)
	}
	if r, _ := Verdict(2, 190); r != "" {
		t.Errorf("a glitch flagged: %q", r)
	}
	if r, _ := Verdict(86, 190); !strings.Contains(r, "86 of the first 190") {
		t.Errorf("decode order not flagged: %q", r)
	}
	if !Repairable("mov,mp4,m4a,3gp,3g2,mj2", "/x/a.mp4") || Repairable("matroska,webm", "/x/a.mkv") {
		t.Error("Repairable")
	}
	if Checked("avi", "/x/a.avi") || !Checked("matroska,webm", "/x/a.mkv") {
		t.Error("Checked")
	}
}
