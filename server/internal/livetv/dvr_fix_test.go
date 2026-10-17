package livetv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A series airing that moved in the guide (its old slot now inside another programme, with
// nothing starting there) is dropped and the new airing scheduled, rather than recording
// the old slot and missing the end.
func TestDVRSeriesFollowsMovedAiring(t *testing.T) {
	s, _, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx)
	r := &Recorder{Live: s, Dir: t.TempDir()}
	list, _ := s.Channels(ctx, uid, Filter{})
	news := list[0]
	weather := news.Next
	if _, _, err := r.Schedule(ctx, uid, news.ID, weather.Start, true, false); err != nil {
		t.Fatal(err)
	}
	// The programme before runs 10 minutes longer; Weather starts 10 minutes later.
	fmtT := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	shift := 10 * time.Minute
	s.DB.Exec(`UPDATE live_programmes SET stop = ? WHERE channel_id = ? AND start = ?`, fmtT(weather.Start.Add(shift)), news.ID, fmtT(news.Now.Start))
	s.DB.Exec(`UPDATE live_programmes SET start = ?, stop = ? WHERE channel_id = ? AND start = ?`,
		fmtT(weather.Start.Add(shift)), fmtT(weather.Stop.Add(shift)), news.ID, fmtT(weather.Start))
	r.Tick(ctx)
	var got []Recording
	for _, x := range must(r.List(ctx)) {
		if x.Status == StatusScheduled {
			got = append(got, x)
		}
	}
	if len(got) != 1 || !got[0].Start.Equal(weather.Start.Add(shift)) || !got[0].Stop.Equal(weather.Stop.Add(shift)) {
		t.Errorf("scheduled after the move: %+v", got)
	}
}

// A programme that has already ended can't be scheduled (it would only fail as "missed").
func TestDVRScheduleEnded(t *testing.T) {
	s, _, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx)
	list, _ := s.Channels(ctx, uid, Filter{})
	news := list[0]
	r := &Recorder{Live: s, Dir: t.TempDir(), Now: func() time.Time { return news.Next.Stop.Add(time.Minute) }}
	if _, _, err := r.Schedule(ctx, uid, news.ID, news.Next.Start, false, false); err != ErrProgrammeNotFound {
		t.Errorf("ended programme: %v", err)
	}
}

// When remuxing fails the longest part is kept as MPEG-TS, never over an earlier recording.
func TestDVRJoinFallbackKeepsEarlier(t *testing.T) {
	fail, err := exec.LookPath("false")
	if err != nil {
		t.Skip("no false")
	}
	dir := t.TempDir()
	work := filepath.Join(dir, ".recording", "1")
	os.MkdirAll(work, 0o755)
	part := filepath.Join(work, "part-000.ts")
	os.WriteFile(part, []byte("new"), 0o644)
	dest := filepath.Join(dir, "TV", "Show", "Show - 2026-10-02.mkv")
	os.MkdirAll(filepath.Dir(dest), 0o755)
	earlier := filepath.Join(dir, "TV", "Show", "Show - 2026-10-02.ts")
	os.WriteFile(earlier, []byte("earlier"), 0o644)
	r := &Recorder{FFmpeg: fail}
	got, err := r.join(context.Background(), []string{part}, dest)
	if err != nil || got == earlier {
		t.Fatalf("join: %s %v", got, err)
	}
	if b, _ := os.ReadFile(earlier); string(b) != "earlier" {
		t.Error("earlier recording overwritten")
	}
	if b, _ := os.ReadFile(got); string(b) != "new" {
		t.Errorf("kept part: %q", b)
	}
}
