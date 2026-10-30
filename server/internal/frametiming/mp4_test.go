package frametiming

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
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
