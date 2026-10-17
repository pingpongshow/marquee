package optimize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/library"
	"marquee/internal/mediatrash"
	"marquee/internal/settings"
)

// keepEnv is one clip queued with a small preset; it returns the service, the clip, the
// job and the media folder.
func keepEnv(t *testing.T, keep bool, ffmpeg string) (*env, *Service, string, int64, string) {
	t.Helper()
	e := newEnv(t)
	media := filepath.Join(e.dir, "media")
	p := filepath.Join(media, "K.mkv")
	clip(t, p, "4")
	fi, _ := os.Stat(p)
	_, file := e.add(fileSpec{path: p, size: fi.Size(), w: 320, h: 240, codec: "mpeg2video", available: true})
	e.set(func(c *settings.Settings) {
		c.Optimize.MinSavingsPercent = 20
		c.Optimize.KeepOriginals = keep
		c.Optimize.Presets = []settings.OptimizePreset{{ID: "tiny", Name: "Tiny", Codec: "hevc", Quality: "small", Audio: "aac_stereo", HDR: "keep"}}
	})
	s := &Service{DB: e.db, Settings: e.settings, FFmpeg: ffmpeg, LookPath: noLook, Trash: &mediatrash.Service{DB: e.db}, Tick: 20 * time.Millisecond}
	s.HasEncoder = func(name string) bool { return name == "libx265" || name == "libx264" }
	if q, _, err := s.Enqueue(context.Background(), []int64{file}, "tiny"); err != nil || q != 1 {
		t.Fatalf("enqueue: %d %v", q, err)
	}
	var id int64
	e.db.QueryRow(`SELECT id FROM optimize_jobs`).Scan(&id)
	return e, s, p, id, media
}

func (e *env) finished(t *testing.T, id int64) (string, string) {
	t.Helper()
	waitFor(t, "the job", func() bool {
		st, _ := e.state(id)
		return st != StateQueued && st != StateRunning && st != StateVerifying
	})
	return e.state(id)
}

func trashed(media string) []string {
	got, _ := filepath.Glob(filepath.Join(media, library.TrashDir, "*", "K*"))
	return got
}

// keepOriginals off (the default): the original is deleted once the new file is verified
// and swapped in, and can't be restored. On: it goes to the media trash, restorable.
func TestOriginalDeletedOrKept(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(map[bool]string{false: "deleted", true: "kept"}[keep], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e, s, p, id, media := keepEnv(t, keep, "")
			go s.Run(ctx)
			if st, msg := e.finished(t, id); st != StateDone {
				t.Fatalf("job: %s %q", st, msg)
			}
			if src, err := s.Probe(ctx, p); err != nil || src.VideoCodec != "hevc" {
				t.Fatalf("new file in place: %+v %v", src, err)
			}
			pg, err := s.Jobs(ctx, true, 0, 10)
			if err != nil || len(pg.Jobs) != 1 {
				t.Fatalf("history: %+v %v", pg, err)
			}
			j := pg.Jobs[0]
			if j.OriginalDeleted == keep || j.CanRestore != keep {
				t.Fatalf("job: deleted %v, can restore %v", j.OriginalDeleted, j.CanRestore)
			}
			if got := trashed(media); keep != (len(got) == 1) || len(got) > 1 {
				t.Fatalf("trash: %v", got)
			}
			err = s.Restore(ctx, id)
			if keep {
				if err != nil {
					t.Fatalf("restore: %v", err)
				}
				if src, err := s.Probe(ctx, p); err != nil || src.VideoCodec != "mpeg2video" {
					t.Fatalf("original back: %+v %v", src, err)
				}
			} else if !errors.Is(err, ErrCantRestore) {
				t.Fatalf("restore without the original: %v", err)
			}
		})
	}
}

// A result that fails verification is thrown away and the original stays exactly as it
// was, even with keepOriginals off.
func TestVerifyFailureKeepsOriginal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// An FFmpeg whose re-encodes come out 2 s long (the clip is 4 s).
	dir := t.TempDir()
	ff, _, release := fakeFFmpeg(t, dir)
	os.WriteFile(release, nil, 0o644)
	short := filepath.Join(dir, "short-ffmpeg")
	script := "#!/bin/sh\nfor a; do last=\"$a\"; done\n'" + ff + "' \"$@\" || exit $?\ncase \"$last\" in *" + PartSuffix + ")\n" +
		"  '" + ff + "' -v error -y -i \"$last\" -t 2 -map 0 -c copy -f matroska \"$last.cut\" && mv \"$last.cut\" \"$last\";;\nesac\n"
	if err := os.WriteFile(short, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e, s, p, id, media := keepEnv(t, false, short)
	before, _ := os.ReadFile(p)
	go s.Run(ctx)
	if st, msg := e.finished(t, id); st != StateFailed || !strings.Contains(msg, "didn't check out") {
		t.Fatalf("job: %s %q", st, msg)
	}
	if after, _ := os.ReadFile(p); string(after) != string(before) {
		t.Error("the original changed")
	}
	if got := trashed(media); len(got) != 0 {
		t.Errorf("trash: %v", got)
	}
	if _, err := os.Stat(p + PartSuffix); !os.IsNotExist(err) {
		t.Error("work file left behind")
	}
}
