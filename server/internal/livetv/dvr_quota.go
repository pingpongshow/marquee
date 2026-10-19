package livetv

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

// DVR quotas and permissions (LIVE-5): a recording belongs to whoever scheduled it (a
// series' airings to the series' owner), and counts against their quota with its actual
// size on disk, finished or in progress, wherever it is.

// Usage is how much a user's recordings take, with the episodes their followed series
// saved (counted in Bytes too).
type Usage struct {
	UserID     int64
	Bytes      int64
	Recordings int
	// FollowedBytes: of Bytes, the followed series' saved episodes.
	FollowedBytes int64
	// QuotaBytes is their quota (0 = unlimited).
	QuotaBytes int64
}

// Over reports whether they're at or over their quota.
func (u Usage) Over() bool { return u.QuotaBytes > 0 && u.Bytes >= u.QuotaBytes }

func (r *Recorder) quota(ctx context.Context, userID int64) int64 {
	if r.QuotaBytes == nil {
		return 0
	}
	return r.QuotaBytes(ctx, userID)
}

func (r *Recorder) mayRecord(ctx context.Context, userID int64) bool {
	return r.CanRecord == nil || r.CanRecord(ctx, userID)
}

// pausedOwners are the owners of series who may no longer record.
func (r *Recorder) pausedOwners(ctx context.Context) (map[int64]bool, error) {
	out := map[int64]bool{}
	if r.CanRecord == nil {
		return out, nil
	}
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT DISTINCT user_id FROM dvr_rules`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if !r.CanRecord(ctx, id) {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// sizeOf is a recording's size on disk now: its file once finished (commercial cutting
// changes it), the parts written so far while recording.
func (r *Recorder) sizeOf(x Recording) int64 {
	switch {
	case x.Status == StatusCompleted && x.Path != "":
		if fi, err := os.Stat(x.Path); err == nil {
			return fi.Size()
		}
		return 0
	case x.Status == StatusRecording || x.Status == StatusScheduled:
		var n int64
		for _, root := range r.Roots(context.Background()) {
			parts, _ := filepath.Glob(filepath.Join(root, ".recording", fmt.Sprint(x.ID), "part-*.ts"))
			for _, p := range parts {
				if fi, err := os.Stat(p); err == nil {
					n += fi.Size()
				}
			}
		}
		return n
	}
	return x.Size
}

// owned lists a user's recordings that take space (finished, or recording now).
func (r *Recorder) owned(ctx context.Context, userID int64) ([]Recording, error) {
	q := `SELECT ` + recCols + ` FROM dvr_recordings r WHERE r.status IN ('completed', 'recording')`
	args := []any{}
	if userID > 0 {
		q += ` AND r.user_id = ?`
		args = append(args, userID)
	}
	rows, err := r.Live.DB.QueryContext(ctx, q+` ORDER BY r.start`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recording
	for rows.Next() {
		x, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// UserUsage is how much one user's recordings (and followed saves) take, and their quota.
func (r *Recorder) UserUsage(ctx context.Context, userID int64) (Usage, error) {
	list, err := r.owned(ctx, userID)
	if err != nil {
		return Usage{}, err
	}
	u := Usage{UserID: userID, QuotaBytes: r.quota(ctx, userID)}
	for _, x := range list {
		u.Bytes += r.sizeOf(x)
		u.Recordings++
	}
	if r.FollowedBytes != nil {
		u.FollowedBytes = r.FollowedBytes(ctx)[userID]
		u.Bytes += u.FollowedBytes
	}
	return u, nil
}

// AllUsage is every user's usage who has recordings or followed saves, by user id.
func (r *Recorder) AllUsage(ctx context.Context) (map[int64]Usage, error) {
	list, err := r.owned(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := map[int64]Usage{}
	for _, x := range list {
		u, ok := out[x.UserID]
		if !ok {
			u = Usage{UserID: x.UserID, QuotaBytes: r.quota(ctx, x.UserID)}
		}
		u.Bytes += r.sizeOf(x)
		u.Recordings++
		out[x.UserID] = u
	}
	if r.FollowedBytes != nil {
		for id, b := range r.FollowedBytes(ctx) {
			u, ok := out[id]
			if !ok {
				u = Usage{UserID: id, QuotaBytes: r.quota(ctx, id)}
			}
			u.FollowedBytes = b
			u.Bytes += b
			out[id] = u
		}
	}
	return out, nil
}

func (r *Recorder) action() string {
	if r.QuotaAction == nil {
		return "block"
	}
	return r.QuotaAction()
}

// atQuota reports whether a user can't schedule more: at or over quota, and (when the
// oldest watched recordings make room) nothing to remove that would bring them under.
func (r *Recorder) atQuota(ctx context.Context, userID int64) bool {
	u, err := r.UserUsage(ctx, userID)
	if err != nil || !u.Over() {
		return false
	}
	if r.action() != "deleteOldest" || r.Trash == nil {
		return true
	}
	var free int64
	for _, x := range r.removable(ctx, userID) {
		free += r.sizeOf(x)
	}
	return u.Bytes-free >= u.QuotaBytes
}

// removable are a user's finished recordings they've watched that nobody is watching now,
// oldest first: what DVRQuotaDeleteOldest may remove.
func (r *Recorder) removable(ctx context.Context, userID int64) []Recording {
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT `+recCols+` FROM dvr_recordings r
		WHERE r.user_id = ? AND r.status = 'completed' AND r.path <> ''
		  AND EXISTS (SELECT 1 FROM media_files f JOIN media_versions v ON v.id = f.version_id
		      JOIN user_item_state s ON s.item_id = v.item_id AND s.user_id = r.user_id
		      WHERE f.path = r.path AND s.play_count > 0)
		ORDER BY r.start, r.id`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Recording
	for rows.Next() {
		x, err := scanRec(rows)
		if err != nil {
			return out
		}
		if r.Playing != nil && r.Playing(x.Path) {
			continue
		}
		out = append(out, x)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// makeRoom moves a user's oldest watched recordings to the media trash until they're
// under quota, returning the titles removed.
func (r *Recorder) makeRoom(ctx context.Context, u Usage) []string {
	var removed []string
	for _, x := range r.removable(ctx, u.UserID) {
		if !u.Over() {
			break
		}
		size := r.sizeOf(x)
		if err := r.Trash(ctx, x.Path); err != nil {
			slog.Warn("dvr: quota: moving a recording to the trash", "id", x.ID, "path", x.Path, "err", err)
			continue
		}
		if err := r.forget(ctx, x); err != nil {
			slog.Warn("dvr: quota: forgetting a recording", "id", x.ID, "err", err)
		}
		slog.Info("dvr: quota: moved a watched recording to the media trash", "id", x.ID, "title", x.Title, "user", u.UserID)
		u.Bytes -= size
		removed = append(removed, x.Title)
	}
	return removed
}

// mayStart checks a due recording's owner may record it, returning why not ("" = go). A
// recording resumed after a restart carries on whatever the quota (a show isn't cut off
// half way).
func (r *Recorder) mayStart(ctx context.Context, id int64) string {
	x, err := r.Get(ctx, id)
	if err != nil {
		return ""
	}
	if !r.mayRecord(ctx, x.UserID) {
		return msgRecordingOff
	}
	if r.sizeOf(x) > 0 {
		return "" // resumed: parts were written before the restart
	}
	u, err := r.UserUsage(ctx, x.UserID)
	if err != nil || !u.Over() {
		return ""
	}
	gb := func(b int64) string { return fmt.Sprintf("%.1f GB", float64(b)/bytesPerGB) }
	if r.action() == "deleteOldest" && r.Trash != nil {
		if removed := r.makeRoom(ctx, u); len(removed) > 0 {
			r.quotaAlert(x, fmt.Sprintf("%s was over their %s recording quota, so %d watched recording(s) went to the media trash to record %q: %v.",
				x.OwnerName, gb(u.QuotaBytes), len(removed), x.Title, removed))
			if u2, err := r.UserUsage(ctx, x.UserID); err == nil && !u2.Over() {
				return ""
			}
		}
	}
	followed := ""
	if u.FollowedBytes > 0 {
		followed = fmt.Sprintf(" (%s of it episodes saved for followed series)", gb(u.FollowedBytes))
	}
	r.quotaAlert(x, fmt.Sprintf("%q wasn't recorded: %s is using %s%s of their %s recording quota.", x.Title, x.OwnerName, gb(u.Bytes), followed, gb(u.QuotaBytes)))
	return msgOverQuota
}

func (r *Recorder) quotaAlert(x Recording, msg string) {
	if r.OnQuota != nil {
		r.OnQuota(x.UserID, x.OwnerName, x.Title, msg)
	}
}

// QuotaMessage is the error a recording refused for its owner's quota carries.
func QuotaMessage() string { return msgOverQuota }
