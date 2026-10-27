package optimize

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"marquee/internal/encode"
	"marquee/internal/playback"
	"marquee/internal/settings"
)

type probeOut struct {
	Streams []struct {
		Index       int    `json:"index"`
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Channels    int    `json:"channels"`
		PixFmt      string `json:"pix_fmt"`
		Profile     string `json:"profile"`
		BitRate     string `json:"bit_rate"`
		Transfer    string `json:"color_transfer"`
		Disposition struct {
			Default     int `json:"default"`
			Comment     int `json:"comment"`
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		Tags struct {
			Title string `json:"title"`
			BPS   string `json:"BPS"`
			BPSEn string `json:"BPS-eng"`
		} `json:"tags"`
		SideData []struct {
			Type      string `json:"side_data_type"`
			DVProfile int    `json:"dv_profile"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe reads a file's streams with ffprobe.
func (s *Service) Probe(ctx context.Context, path string) (Source, error) {
	src := Source{Path: path, VideoIndex: -1}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, or(s.FFprobe, "ffprobe"), "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	cmd.Stderr = &encode.LimitWriter{W: &stderr, N: 4096}
	out, err := cmd.Output()
	if err != nil {
		return src, fmt.Errorf("ffprobe couldn't read it: %s", encode.LastLine(stderr.String(), err))
	}
	var p probeOut
	if err := json.Unmarshal(out, &p); err != nil {
		return src, fmt.Errorf("ffprobe's answer wasn't understood: %v", err)
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		src.DurationMS = int64(math.Round(d * 1000))
	}
	for _, st := range p.Streams {
		switch st.CodecType {
		case "video":
			if st.Disposition.AttachedPic == 1 || src.VideoIndex >= 0 {
				continue
			}
			src.VideoIndex, src.Width, src.Height, src.PixFmt, src.Transfer, src.VideoCodec = st.Index, st.Width, st.Height, st.PixFmt, st.Transfer, st.CodecName
			for _, sd := range st.SideData {
				if sd.DVProfile > 0 {
					src.DVProfile = sd.DVProfile
				}
			}
		case "audio":
			src.Audio = append(src.Audio, Stream{Index: st.Index, Codec: st.CodecName, Channels: st.Channels, Title: st.Tags.Title,
				Profile: st.Profile, Kbps: streamKbps(st.BitRate, st.Tags.BPS, st.Tags.BPSEn),
				Default: st.Disposition.Default == 1, Comment: st.Disposition.Comment == 1 || strings.Contains(strings.ToLower(st.Tags.Title), "commentary")})
		case "subtitle":
			src.Subtitles = append(src.Subtitles, Stream{Index: st.Index, Codec: st.CodecName, Title: st.Tags.Title, Default: st.Disposition.Default == 1})
		case "attachment":
			src.Attachments++
		}
	}
	if src.VideoIndex < 0 {
		return src, errors.New("it has no video")
	}
	if src.DurationMS <= 0 {
		return src, errors.New("its length is unknown")
	}
	return src, nil
}

// streamKbps is a stream's bitrate in kb/s from ffprobe's bit_rate or Matroska's BPS
// statistics tags (mkvmerge writes them; FFmpeg's bit_rate is often missing in MKV); 0 =
// unknown.
func streamKbps(vals ...string) int {
	for _, v := range vals {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
			return int(n / 1000)
		}
	}
	return 0
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// encoders are the encoders to try for a codec, best first: NVENC only while it leaves a
// session free for playback, and only encoders FFmpeg has for the codec.
func (s *Service) encoders(codec string) []string {
	list := []string{"software"}
	if s.Encoders != nil {
		if e := s.Encoders(); len(e) > 0 {
			list = e
		}
	}
	limit := s.nvencLimit()
	s.mu.Lock()
	inUse := s.nvencInUseLocked()
	s.mu.Unlock()
	var out []string
	for _, e := range list {
		if e == "nvenc" && limit > 0 && s.playbackNVENC()+inUse+1 >= limit {
			continue
		}
		if s.HasEncoder != nil && !s.HasEncoder(encode.EncoderName(e, codec)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// AV1Available reports whether FFmpeg has an AV1 encoder (SVT-AV1, or NVENC / Quick Sync
// AV1 on hardware that has it).
func (s *Service) AV1Available() bool {
	if s.HasEncoder == nil {
		return false
	}
	for _, e := range []string{"libsvtav1", "av1_nvenc", "av1_qsv"} {
		if s.HasEncoder(e) {
			return true
		}
	}
	return false
}

type jobRow struct {
	fileID, libID, before int64
	path, title           string
	preset                settings.OptimizePreset
}

func (s *Service) loadJob(ctx context.Context, id int64) (jobRow, error) {
	var r jobRow
	var fileID sql.NullInt64
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT file_id, library_id, before_size, path, title, preset FROM optimize_jobs WHERE id = ?`, id).
		Scan(&fileID, &r.libID, &r.before, &r.path, &r.title, &raw); err != nil {
		return r, err
	}
	r.fileID = fileID.Int64
	if err := json.Unmarshal([]byte(raw), &r.preset); err != nil {
		return r, fmt.Errorf("the job's preset can't be read: %w", err)
	}
	return r, nil
}

// end records a job's end: failed, skipped or cancelled, with why. A failure is reported
// to OnFailed (admin alerts); a skip isn't.
func (s *Service) end(id int64, state, msg string) {
	s.DB.Exec(`UPDATE optimize_jobs SET state = ?, error = ?, finished_at = ? WHERE id = ?`, state, msg, s.stamp(), id)
	if state != StateFailed || s.OnFailed == nil {
		return
	}
	f := Failure{JobID: id, Error: msg}
	var fileID sql.NullInt64
	s.DB.QueryRow(`SELECT file_id, title, path FROM optimize_jobs WHERE id = ?`, id).Scan(&fileID, &f.Title, &f.Path)
	f.FileID = fileID.Int64
	s.OnFailed(f)
}

// Failure is a conversion that failed, as OnFailed hears it.
type Failure struct {
	JobID, FileID int64
	Title, Path   string
	Error         string
}

func (s *Service) setState(j *job, state string) {
	s.mu.Lock()
	j.state = state
	s.mu.Unlock()
	s.DB.Exec(`UPDATE optimize_jobs SET state = ? WHERE id = ?`, state, j.id)
}

// process makes, checks and swaps in one file.
func (s *Service) process(ctx context.Context, j *job) {
	r, err := s.loadJob(ctx, j.id)
	if err != nil {
		s.end(j.id, StateFailed, err.Error())
		return
	}
	// The file as the library has it now (it may have moved, or gone).
	var path string
	var size int64
	var available bool
	err = s.DB.QueryRowContext(ctx, `SELECT path, size, available FROM media_files WHERE id = ?`, r.fileID).Scan(&path, &size, &available)
	switch {
	case r.fileID == 0 || errors.Is(err, sql.ErrNoRows):
		s.end(j.id, StateFailed, "The file isn't in the library any more")
		return
	case err != nil:
		s.end(j.id, StateFailed, err.Error())
		return
	case !available:
		s.end(j.id, StateFailed, "The file isn't on disk (missing or on an offline drive)")
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		s.end(j.id, StateFailed, "The file can't be read: "+err.Error())
		return
	}
	before := fi.Size()
	s.DB.Exec(`UPDATE optimize_jobs SET path = ?, before_size = ? WHERE id = ?`, path, before, j.id)

	src, err := s.Probe(ctx, path)
	if err != nil {
		s.end(j.id, StateFailed, "The file can't be read: "+err.Error())
		return
	}
	plan, err := NewPlan(src, r.preset)
	if errors.Is(err, ErrUnsupported) {
		s.end(j.id, StateSkipped, strings.TrimPrefix(err.Error(), ErrUnsupported.Error()+": "))
		return
	}
	if err != nil {
		s.end(j.id, StateFailed, err.Error())
		return
	}
	if plan.Final != path {
		if _, err := os.Stat(plan.Final); err == nil {
			s.end(j.id, StateFailed, "There's already a file named "+plan.Final)
			return
		}
	}
	part := path + PartSuffix
	defer os.Remove(part)

	err = s.encode(ctx, j, src, plan, part)
	if err != nil {
		os.Remove(part)
		switch {
		case errors.Is(j.why, errCancelled):
			s.end(j.id, StateCancelled, "")
		case errors.Is(j.why, errYield), errors.Is(j.why, errRestart):
			// Back in the queue where it was; it starts again when it can.
			slog.Info("optimize: job put back in the queue", "path", path, "why", j.why)
			s.DB.Exec(`UPDATE optimize_jobs SET state = 'queued', progress = 0 WHERE id = ?`, j.id)
		case ctx.Err() != nil:
			// Shutting down: Run puts it back in the queue next time.
		default:
			slog.Warn("optimize: encode failed", "path", path, "err", err)
			s.end(j.id, StateFailed, "Couldn't re-encode it (the original is unchanged): "+err.Error())
		}
		return
	}

	s.setState(j, StateVerifying)
	if err := s.verify(ctx, part, src, plan); err != nil {
		if ctx.Err() != nil {
			s.cancelledOrRequeued(j)
			return
		}
		s.end(j.id, StateFailed, "The re-encoded file didn't check out, so the original was kept: "+err.Error())
		return
	}
	pf, err := os.Stat(part)
	if err != nil {
		s.end(j.id, StateFailed, err.Error())
		return
	}
	after := pf.Size()
	minPct := s.cfg().MinSavingsPercent
	saved := 0.0
	if before > 0 {
		saved = float64(before-after) / float64(before) * 100
	}
	if saved < float64(minPct) {
		s.DB.Exec(`UPDATE optimize_jobs SET after_size = ? WHERE id = ?`, after, j.id)
		msg := fmt.Sprintf("Only %.0f%% smaller (%s → %s), under the %d%% that's worth it; the original was kept", math.Max(saved, 0), Bytes(before), Bytes(after), minPct)
		if saved < 0 {
			msg = fmt.Sprintf("It came out bigger (%s → %s); the original was kept", Bytes(before), Bytes(after))
		}
		s.end(j.id, StateSkipped, msg)
		return
	}
	// Never swap a file someone's watching: wait until they stop (cancelling still works).
	for s.Playing != nil && s.Playing(r.fileID) {
		s.mu.Lock()
		j.note = "Waiting until nobody is watching it"
		s.mu.Unlock()
		retry := s.BusyRetry
		if retry == 0 {
			retry = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			s.cancelledOrRequeued(j)
			return
		case <-time.After(retry):
		}
	}
	s.mu.Lock()
	j.note = ""
	s.mu.Unlock()
	if ctx.Err() != nil {
		s.cancelledOrRequeued(j)
		return
	}
	if err := s.swap(context.WithoutCancel(ctx), j.id, r.fileID, r.libID, path, part, plan.Final, after); err != nil {
		slog.Warn("optimize: swap failed; the original is unchanged", "path", path, "err", err)
		s.end(j.id, StateFailed, "Couldn't put the new file in place (the original is unchanged): "+err.Error())
		return
	}
	slog.Info("optimize: done", "path", plan.Final, "before", before, "after", after)
}

func (s *Service) cancelledOrRequeued(j *job) {
	if errors.Is(j.why, errCancelled) {
		s.end(j.id, StateCancelled, "")
	}
}

// encode runs FFmpeg with each encoder in turn until one works.
func (s *Service) encode(ctx context.Context, j *job, src Source, plan Plan, out string) error {
	encs := s.encoders(plan.Codec)
	if len(encs) == 0 {
		return fmt.Errorf("this server has no %s encoder", strings.ToUpper(plan.Codec))
	}
	var err error
	for i := 0; i < len(encs); i++ {
		enc := encs[i]
		s.mu.Lock()
		j.encoder, j.prog = enc, encode.Progress{}
		s.mu.Unlock()
		s.DB.Exec(`UPDATE optimize_jobs SET encoder = ?, progress = 0 WHERE id = ?`, enc, j.id)
		args := plan.Args(src, enc, playback.EncoderPreset(enc, "quality"), s.QSVDevice, out)
		name, args := encode.LowPriority(s.LookPath, or(s.FFmpeg, "ffmpeg"), args...)
		err = encode.Run(ctx, name, args, src.DurationMS, func(p encode.Progress) { s.progress(j, p) }, func(cmd *exec.Cmd) {
			s.mu.Lock()
			j.cmd = cmd
			s.mu.Unlock()
		})
		s.mu.Lock()
		j.cmd, j.stopped = nil, false
		s.mu.Unlock()
		if err == nil || ctx.Err() != nil {
			return err
		}
		os.Remove(out)
		// The all-GPU route can fail on a file the GPU otherwise handles, e.g. when the
		// decoder hands back a frame it decoded on the CPU, which scale_cuda can't take
		// ("Function not implemented"). Try NVENC again with the filters on the CPU before
		// giving the job to a slower encoder.
		if plan.FullGPU(src, enc) {
			slog.Info("optimize: all-GPU conversion failed; trying NVENC with the filters on the CPU", "path", src.Path, "err", err)
			plan.CPUFilters = true
			i--
			continue
		}
		slog.Info("optimize: encoder failed; trying the next", "encoder", enc, "path", src.Path, "err", err)
	}
	return err
}

func (s *Service) progress(j *job, p encode.Progress) {
	s.mu.Lock()
	j.prog = p
	save := time.Since(j.saved) > 5*time.Second
	if save {
		j.saved = time.Now()
	}
	s.mu.Unlock()
	if save {
		s.DB.Exec(`UPDATE optimize_jobs SET progress = ? WHERE id = ?`, p.Fraction, j.id)
	}
}

// verify checks the result before it replaces anything: FFprobe reads it, it's as long as
// the original (within 1%), it has the streams the plan maps, and a few seconds at the
// start, middle and end of the picture decode cleanly.
func (s *Service) verify(ctx context.Context, path string, src Source, plan Plan) error {
	got, err := s.Probe(ctx, path)
	if err != nil {
		return err
	}
	if d := got.DurationMS - src.DurationMS; d > src.DurationMS/100 || -d > src.DurationMS/100 {
		return fmt.Errorf("it is %s long; the original is %s", clock(got.DurationMS), clock(src.DurationMS))
	}
	if len(got.Audio) != len(plan.Audio) || len(got.Subtitles) != len(plan.Subtitles) {
		return fmt.Errorf("it has %d audio and %d subtitle streams; expected %d and %d",
			len(got.Audio), len(got.Subtitles), len(plan.Audio), len(plan.Subtitles))
	}
	if b, ok := boxes[plan.Preset.MaxHeight]; ok && (got.Width > b[0] || got.Height > b[1]) {
		return fmt.Errorf("it is %d×%d; expected at most %d×%d", got.Width, got.Height, b[0], b[1])
	}
	// The picture can end before the file does (audio or subtitles running on after the last
	// frame), with nothing to decode after it in the original either. The samples stop where
	// the original's picture stops, so a result whose picture is cut short still fails.
	end := float64(got.DurationMS) / 1000
	if ms, ok := encode.VideoEnd(ctx, or(s.FFprobe, "ffprobe"), src.Path, src.VideoIndex, src.DurationMS); ok {
		end = min(end, float64(ms)/1000)
	}
	for _, at := range []float64{0, end * 0.5, max(0, end-min(30, end/3))} {
		if err := encode.DecodeCheck(ctx, or(s.FFmpeg, "ffmpeg"), path, at, 5, true); err != nil {
			return fmt.Errorf("%s in doesn't decode: %v", clock(int64(at*1000)), err)
		}
	}
	return nil
}

// swap puts the new file in the original's place, keeping the library's record (so the
// item keeps its id, watch history and metadata): the original moves to the media trash,
// the record is pointed at the new file (same name, or .mkv when the container changed)
// to be read again by the next scan, and the library is rescanned. Unless keepOriginals
// is on, the original is then deleted, only once the new file is in place (it was verified
// before swap was called); anything failing before that leaves the original as it was.
func (s *Service) swap(ctx context.Context, jobID, fileID, libID int64, path, part, final string, after int64) error {
	if s.Trash == nil {
		return errors.New("the media trash isn't available")
	}
	original, undo, err := s.Trash.Stash(ctx, fileID, "optimized")
	if err != nil {
		return fmt.Errorf("couldn't move the original to the media trash: %w", err)
	}
	if err := os.Rename(part, final); err != nil {
		undo()
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE media_files SET path = ?, size = -1, mtime = 0 WHERE id = ?`, final, fileID); err != nil {
		slog.Warn("optimize: couldn't point the library at the new file; the scan will", "path", final, "err", err)
	}
	deleted := false
	if !s.cfg().KeepOriginals {
		if err := os.Remove(original); err != nil && !os.IsNotExist(err) {
			slog.Warn("optimize: couldn't delete the original; it stays in the media trash", "path", original, "err", err)
		} else {
			deleted, original = true, ""
		}
	}
	s.DB.ExecContext(ctx, `UPDATE optimize_jobs SET state = 'done', progress = 1, after_size = ?, final_path = ?, original = ?, original_deleted = ?,
		error = '', finished_at = ? WHERE id = ?`, after, final, original, deleted, s.stamp(), jobID)
	if s.Rescan != nil {
		s.Rescan(libID)
	}
	return nil
}

// Restore puts an optimized file's original back while it's still in the media trash;
// the optimized file goes to the trash instead.
func (s *Service) Restore(ctx context.Context, id int64) error {
	var state, path, final, original string
	var fileID sql.NullInt64
	var libID int64
	err := s.DB.QueryRowContext(ctx, `SELECT state, path, final_path, original, file_id, library_id FROM optimize_jobs WHERE id = ?`, id).
		Scan(&state, &path, &final, &original, &fileID, &libID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if state != StateDone || original == "" || !fileID.Valid {
		return ErrCantRestore
	}
	if _, err := os.Stat(original); err != nil {
		return ErrCantRestore
	}
	if s.Playing != nil && s.Playing(fileID.Int64) {
		return ErrBusy
	}
	if s.Trash == nil {
		return errors.New("the media trash isn't available")
	}
	if path != final {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("there's a file at %s already", path)
		}
	}
	_, undo, err := s.Trash.Stash(ctx, fileID.Int64, "original restored")
	if err != nil {
		return fmt.Errorf("couldn't move the optimized file to the media trash: %w", err)
	}
	if err := os.Rename(original, path); err != nil {
		undo()
		return fmt.Errorf("couldn't put the original back: %w", err)
	}
	wctx := context.WithoutCancel(ctx)
	s.DB.ExecContext(wctx, `UPDATE media_files SET path = ?, size = -1, mtime = 0 WHERE id = ?`, path, fileID.Int64)
	s.DB.ExecContext(wctx, `UPDATE optimize_jobs SET state = 'restored', original = '', finished_at = ? WHERE id = ?`, s.stamp(), id)
	if s.Rescan != nil {
		s.Rescan(libID)
	}
	slog.Info("optimize: original restored", "path", path)
	return nil
}

// Job is a job as the queue and history show it.
type Job struct {
	ID             int64
	FileID, ItemID int64
	Title, Path    string
	PresetID       string
	PresetName     string
	State          string
	Progress       float64
	FPS, Speed     float64
	ETASeconds     int64
	Encoder        string
	BeforeSize     int64
	AfterSize      int64
	Error          string
	Paused         string
	CanRestore     bool
	// OriginalDeleted: the original was deleted after the swap (keepOriginals off).
	OriginalDeleted bool
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	durationMS      int64
}

// JobPage is a page of jobs with the totals saved.
type JobPage struct {
	Jobs       []Job
	Total      int
	SavedBytes int64
	Optimized  int
}

// Jobs lists the queue (active and finished jobs not cleared, active first) or the history
// (optimized and restored files, newest first).
func (s *Service) Jobs(ctx context.Context, history bool, offset, limit int) (JobPage, error) {
	where, order := `j.cleared = 0`, `CASE WHEN j.state IN ('running', 'verifying') THEN 0 WHEN j.state = 'queued' THEN 1 ELSE 2 END,
		CASE WHEN j.state IN ('running', 'verifying', 'queued') THEN j.created_at END, j.finished_at DESC, j.id`
	if history {
		where, order = `j.state IN ('done', 'restored')`, `j.finished_at DESC, j.id DESC`
	}
	if limit <= 0 {
		limit = 50
	}
	var out JobPage
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimize_jobs j WHERE `+where).Scan(&out.Total); err != nil {
		return out, err
	}
	s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(before_size - after_size), 0), COUNT(*) FROM optimize_jobs WHERE state = 'done'`).Scan(&out.SavedBytes, &out.Optimized)
	rows, err := s.DB.QueryContext(ctx, `SELECT j.id, COALESCE(j.file_id, 0), COALESCE(j.item_id, 0), j.title, CASE WHEN j.final_path <> '' THEN j.final_path ELSE j.path END,
			j.preset_id, j.preset_name, j.state, j.progress, j.encoder, j.before_size, j.after_size, j.error, j.original, j.original_deleted, j.created_at, j.started_at, j.finished_at,
			COALESCE(f.duration_ms, 0)
		FROM optimize_jobs j LEFT JOIN media_files f ON f.id = j.file_id WHERE `+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, limit, max(offset, 0))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Jobs = []Job{}
	for rows.Next() {
		var jb Job
		var original, created string
		var started, finished sql.NullString
		if err := rows.Scan(&jb.ID, &jb.FileID, &jb.ItemID, &jb.Title, &jb.Path, &jb.PresetID, &jb.PresetName, &jb.State, &jb.Progress, &jb.Encoder,
			&jb.BeforeSize, &jb.AfterSize, &jb.Error, &original, &jb.OriginalDeleted, &created, &started, &finished, &jb.durationMS); err != nil {
			return out, err
		}
		jb.CreatedAt = parseTime(created)
		if t := parseTime(started.String); !t.IsZero() {
			jb.StartedAt = &t
		}
		if t := parseTime(finished.String); !t.IsZero() {
			jb.FinishedAt = &t
		}
		if jb.State == StateDone && original != "" {
			_, err := os.Stat(original)
			jb.CanRestore = err == nil
		}
		if l, ok := s.liveFor(jb.ID); ok {
			jb.Progress, jb.FPS, jb.Speed, jb.Encoder, jb.Paused = l.prog.Fraction, l.prog.FPS, l.prog.Speed, l.encoder, l.paused
			if l.state != "" {
				jb.State = l.state
			}
			if l.prog.Speed > 0 && jb.durationMS > 0 && jb.State == StateRunning {
				jb.ETASeconds = int64((1 - l.prog.Fraction) * float64(jb.durationMS) / 1000 / l.prog.Speed)
			}
		} else if jb.State == StateQueued {
			jb.Paused = s.HoldReason()
		}
		out.Jobs = append(out.Jobs, jb)
	}
	return out, rows.Err()
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Bytes formats a size like 3.2 GB (decimal units, as the web shows them).
func Bytes(n int64) string {
	f := float64(n)
	for _, u := range []string{"B", "KB", "MB", "GB"} {
		if math.Abs(f) < 1000 {
			if u == "B" {
				return fmt.Sprintf("%d B", n)
			}
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1000
	}
	return fmt.Sprintf("%.1f TB", f)
}

func clock(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}
