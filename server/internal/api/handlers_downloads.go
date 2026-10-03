package api

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"marquee/internal/auth"
	"marquee/internal/downloads"
	"marquee/internal/netclass"
)

// originalFile finds the file to download: the requested one, else the best available.
func (h *Handlers) originalFile(ctx context.Context, itemID, fileID int64) (id int64, path string, size int64, err error) {
	q := `SELECT f.id, f.path, f.size FROM media_files f JOIN media_versions v ON v.id = f.version_id
		WHERE v.item_id = ? AND f.available = 1 AND f.part_index = 0`
	args := []any{itemID}
	if fileID > 0 {
		q += ` AND f.id = ?`
		args = append(args, fileID)
	}
	err = h.DB.QueryRowContext(ctx, q+` ORDER BY COALESCE(f.height, 0) DESC LIMIT 1`, args...).Scan(&id, &path, &size)
	return
}

func toAPIDownload(j downloads.Job) Download {
	d := Download{Id: j.ID, ItemId: j.ItemID, Quality: DownloadQuality(j.Quality), Status: DownloadStatus(j.Status),
		Progress: float32(j.Progress), Error: nz(j.Error), CreatedAt: ptr(j.CreatedAt)}
	if j.Status == "ready" {
		d.Size = nz(j.Size)
		d.Url = ptr("/api/v1/download/job/" + j.ID)
		d.FileName = ptr(j.ID + ".mp4")
	}
	return d
}

func (h *Handlers) CreateDownload(ctx context.Context, req CreateDownloadRequestObject) (CreateDownloadResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return CreateDownload401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	if h.Items.Visible(ctx, access(ctx), b.ItemId) != nil {
		return CreateDownload404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if requestInfo(ctx).Class == netclass.Remote && !sess.User.IsAdmin && !sess.User.Restrictions.RemoteAllowed() {
		return CreateDownload403JSONResponse{ForbiddenJSONResponse(apiErr("remote_disabled", "downloading away from home isn't allowed for this account"))}, nil
	}
	var fileID int64
	if b.FileId != nil {
		fileID = *b.FileId
	}
	if b.Quality == CreateDownloadJSONBodyQualityOriginal {
		id, path, size, err := h.originalFile(ctx, b.ItemId, fileID)
		if errors.Is(err, sql.ErrNoRows) {
			return CreateDownload404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no available file"))}, nil
		}
		if err != nil {
			return nil, internal(ctx, "createDownload", err)
		}
		return CreateDownload200JSONResponse{Id: fmt.Sprintf("original-%d", id), ItemId: b.ItemId, Quality: DownloadQualityOriginal, Status: DownloadStatusReady, Progress: 1,
			Size: &size, Url: ptr(fmt.Sprintf("/api/v1/download/original/%d?fileId=%d", b.ItemId, id)), FileName: ptr(filepath.Base(path))}, nil
	}
	j, err := h.Downloads.Create(ctx, sess.User.ID, b.ItemId, fileID, string(b.Quality))
	if err != nil {
		return CreateDownload400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	return CreateDownload200JSONResponse(toAPIDownload(j)), nil
}

func (h *Handlers) ListDownloads(ctx context.Context, _ ListDownloadsRequestObject) (ListDownloadsResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return ListDownloads401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	list, err := h.Downloads.List(ctx, sess.User.ID)
	if err != nil {
		return nil, internal(ctx, "listDownloads", err)
	}
	out := make(ListDownloads200JSONResponse, len(list))
	for i, j := range list {
		out[i] = toAPIDownload(j)
	}
	return out, nil
}

func (h *Handlers) GetDownload(ctx context.Context, req GetDownloadRequestObject) (GetDownloadResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return GetDownload401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	j, err := h.Downloads.Get(ctx, sess.User.ID, req.DownloadId)
	if errors.Is(err, downloads.ErrNotFound) {
		return GetDownload404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "getDownload", err)
	}
	return GetDownload200JSONResponse(toAPIDownload(j)), nil
}

func (h *Handlers) DeleteDownload(ctx context.Context, req DeleteDownloadRequestObject) (DeleteDownloadResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return DeleteDownload401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if strings.HasPrefix(req.DownloadId, "original-") {
		return DeleteDownload204Response{}, nil // nothing kept on the server
	}
	if err := h.Downloads.Delete(ctx, sess.User.ID, req.DownloadId); errors.Is(err, downloads.ErrNotFound) {
		return DeleteDownload404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "deleteDownload", err)
	}
	return DeleteDownload204Response{}, nil
}

// SyncProgress records progress made offline.
func (h *Handlers) SyncProgress(ctx context.Context, req SyncProgressRequestObject) (SyncProgressResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return SyncProgress401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if h.Items.Visible(ctx, access(ctx), req.ItemId) != nil {
		return SyncProgress404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	b := req.Body
	at := time.Now().UTC()
	if b.PlayedAt != nil && b.PlayedAt.Before(at) {
		at = b.PlayedAt.UTC()
	}
	when := at.Format("2006-01-02T15:04:05.000Z")
	watched := b.Watched != nil && *b.Watched
	var err error
	if watched {
		_, err = h.DB.ExecContext(ctx, `INSERT INTO user_item_state (user_id, item_id, play_count, view_offset_ms, last_viewed_at)
			VALUES (?, ?, 1, 0, ?) ON CONFLICT(user_id, item_id) DO UPDATE SET play_count = play_count + 1, view_offset_ms = 0,
			last_viewed_at = MAX(COALESCE(last_viewed_at, ''), excluded.last_viewed_at), watchlisted_at = NULL`, sess.User.ID, req.ItemId, when)
		if err == nil {
			_, err = h.DB.ExecContext(ctx, `INSERT INTO play_history (user_id, item_id, device_id, item_title, started_at, stopped_at, position_ms, source)
				SELECT ?, id, ?, title, ?, ?, ?, 'offline' FROM items WHERE id = ?`, sess.User.ID, nullDevice(sess.DeviceID), when, when, b.PositionMs, req.ItemId)
		}
	} else {
		// Only move the resume point forward in time: a stale offline report mustn't undo
		// progress made since on another device.
		_, err = h.DB.ExecContext(ctx, `INSERT INTO user_item_state (user_id, item_id, view_offset_ms, last_viewed_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, item_id) DO UPDATE SET view_offset_ms = excluded.view_offset_ms, last_viewed_at = excluded.last_viewed_at
			WHERE COALESCE(last_viewed_at, '') <= excluded.last_viewed_at`, sess.User.ID, req.ItemId, b.PositionMs, when)
	}
	if err != nil {
		return nil, internal(ctx, "syncProgress", err)
	}
	return SyncProgress204Response{}, nil
}

func nullDevice(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// DownloadFiles serves downloads under /api/v1/download/: original/<item>?fileId=, job/<id>,
// and zip/<season or show> (every episode's original file, uncompressed). Range requests let
// apps resume interrupted single-file downloads.
func (h *Handlers) DownloadFiles() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := auth.SessionFrom(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Same rule as streaming: accounts without remote access can't download away from home.
		if requestInfo(r.Context()).Class == netclass.Remote && !sess.User.IsAdmin && !sess.User.Restrictions.RemoteAllowed() {
			http.Error(w, "downloading away from home isn't allowed for this account", http.StatusForbidden)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/download/")
		var path, name string
		switch {
		case strings.HasPrefix(rest, "original/"):
			itemID, err := strconv.ParseInt(strings.TrimPrefix(rest, "original/"), 10, 64)
			if err != nil || h.Items.Visible(r.Context(), access(r.Context()), itemID) != nil {
				http.NotFound(w, r)
				return
			}
			fileID, _ := strconv.ParseInt(r.URL.Query().Get("fileId"), 10, 64)
			_, p, _, err := h.originalFile(r.Context(), itemID, fileID)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			path, name = p, filepath.Base(p)
		case strings.HasPrefix(rest, "zip/"):
			itemID, err := strconv.ParseInt(strings.TrimPrefix(rest, "zip/"), 10, 64)
			if err != nil || h.Items.Visible(r.Context(), access(r.Context()), itemID) != nil {
				http.NotFound(w, r)
				return
			}
			h.serveZip(w, r, itemID)
			return
		case strings.HasPrefix(rest, "job/"):
			j, err := h.Downloads.Get(r.Context(), sess.User.ID, strings.TrimPrefix(rest, "job/"))
			if err != nil || j.Status != "ready" || j.Path == "" {
				http.NotFound(w, r)
				return
			}
			path, name = j.Path, j.ID+".mp4"
		default:
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.Error(w, "unreadable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		http.ServeContent(w, r, name, st.ModTime(), f)
	})
}

// serveZip streams the original files of a season's or show's episodes (that the person may
// see) as one zip, stored without compression: video doesn't compress, and the download
// starts at once. Shows get a folder per season.
func (h *Handlers) serveZip(w http.ResponseWriter, r *http.Request, itemID int64) {
	ctx := r.Context()
	var typ, title, parent string
	if err := h.DB.QueryRowContext(ctx, `SELECT i.type, i.title, COALESCE(p.title, '') FROM items i LEFT JOIN items p ON p.id = i.parent_id
		WHERE i.id = ?`, itemID).Scan(&typ, &title, &parent); err != nil || (typ != "season" && typ != "show") {
		http.Error(w, "only seasons and shows download as a zip", http.StatusBadRequest)
		return
	}
	col := "parent_id"
	if typ == "show" {
		col = "grandparent_id"
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT e.id, COALESCE(s.title, '') FROM items e LEFT JOIN items s ON s.id = e.parent_id
		WHERE e.`+col+` = ? AND e.type = 'episode' AND e.extra_type IS NULL
		ORDER BY COALESCE(s.idx, 0), COALESCE(e.idx, 0), e.sort_title`, itemID)
	if err != nil {
		http.Error(w, "unreadable", http.StatusInternalServerError)
		return
	}
	type entry struct {
		id     int64
		season string
	}
	var eps []entry
	for rows.Next() {
		var e entry
		if rows.Scan(&e.id, &e.season) == nil {
			eps = append(eps, e)
		}
	}
	rows.Close()
	acc := access(ctx)
	name := title
	if typ == "season" && parent != "" {
		name = parent + " - " + title
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safeName(name)+".zip"))
	zw := zip.NewWriter(w)
	n := 0
	for _, e := range eps {
		if ctx.Err() != nil {
			return
		}
		if h.Items.Visible(ctx, acc, e.id) != nil {
			continue
		}
		_, p, _, err := h.originalFile(ctx, e.id, 0)
		if err != nil {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		st, _ := f.Stat()
		inZip := filepath.Base(p)
		if typ == "show" && e.season != "" {
			inZip = safeName(e.season) + "/" + inZip
		}
		hdr := &zip.FileHeader{Name: inZip, Method: zip.Store}
		if st != nil {
			hdr.Modified = st.ModTime()
		}
		out, err := zw.CreateHeader(hdr)
		if err == nil {
			_, err = io.Copy(out, f)
		}
		f.Close()
		if err != nil {
			return // the connection went away
		}
		n++
	}
	zw.Close()
	slog.InfoContext(ctx, "zip download", "item", name, "files", n)
}

// safeName keeps a title usable as a file name.
func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
}
