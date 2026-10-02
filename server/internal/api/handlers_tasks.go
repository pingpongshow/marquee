package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"marquee/internal/tasks"
)

func toAPIBackup(b tasks.Backup) Backup {
	return Backup{Name: b.Name, Size: b.Size, CreatedAt: b.CreatedAt, Kind: BackupKind(b.Kind)}
}

func (h *Handlers) ListTasks(ctx context.Context, _ ListTasksRequestObject) (ListTasksResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ListTasks401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListTasks403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	list, err := h.Tasks.List(ctx)
	if err != nil {
		return nil, internal(ctx, "tasks", err)
	}
	out := make(ListTasks200JSONResponse, len(list))
	for i, t := range list {
		out[i] = TaskInfo{Id: t.ID, Name: t.Name, Description: t.Description, Schedule: t.Schedule(), Running: t.Running}
		if t.Last != nil {
			out[i].LastRun = &TaskRun{Status: TaskRunStatus(t.Last.Status), StartedAt: t.Last.StartedAt, FinishedAt: t.Last.FinishedAt, Message: nz(t.Last.Message)}
		}
	}
	return out, nil
}

func (h *Handlers) RunTask(ctx context.Context, req RunTaskRequestObject) (RunTaskResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RunTask401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RunTask403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	switch err := h.Tasks.RunNow(ctx, req.TaskId); {
	case errors.Is(err, tasks.ErrUnknownTask):
		return RunTask404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	case errors.Is(err, tasks.ErrTaskRunning):
		return RunTask409JSONResponse{ConflictJSONResponse(apiErr("running", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "runTask", err)
	}
	return RunTask202Response{}, nil
}

func (h *Handlers) ListBackups(ctx context.Context, _ ListBackupsRequestObject) (ListBackupsResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ListBackups401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListBackups403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	list, err := h.Backups.List()
	if err != nil {
		return nil, internal(ctx, "backups", err)
	}
	out := make(ListBackups200JSONResponse, len(list))
	for i, b := range list {
		out[i] = toAPIBackup(b)
	}
	return out, nil
}

func (h *Handlers) CreateBackup(ctx context.Context, _ CreateBackupRequestObject) (CreateBackupResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return CreateBackup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CreateBackup403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b, err := h.Backups.Create(ctx, "manual")
	if err != nil {
		return nil, internal(ctx, "createBackup", err)
	}
	h.Backups.Prune()
	return CreateBackup201JSONResponse(toAPIBackup(b)), nil
}

func (h *Handlers) DownloadBackup(ctx context.Context, req DownloadBackupRequestObject) (DownloadBackupResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return DownloadBackup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return DownloadBackup403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	p, err := h.Backups.Path(req.Name)
	if err != nil {
		return DownloadBackup404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, internal(ctx, "downloadBackup", err)
	}
	st, _ := f.Stat()
	return DownloadBackup200ApplicationoctetStreamResponse{Body: f, ContentLength: st.Size(),
		Headers: DownloadBackup200ResponseHeaders{ContentDisposition: ptr(fmt.Sprintf(`attachment; filename="%s"`, req.Name))}}, nil
}

func (h *Handlers) DeleteBackup(ctx context.Context, req DeleteBackupRequestObject) (DeleteBackupResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return DeleteBackup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return DeleteBackup403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Backups.Delete(req.Name); errors.Is(err, tasks.ErrBackupNotFound) {
		return DeleteBackup404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "deleteBackup", err)
	}
	return DeleteBackup204Response{}, nil
}

func (h *Handlers) RestoreBackup(ctx context.Context, req RestoreBackupRequestObject) (RestoreBackupResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RestoreBackup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RestoreBackup403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Backups.StageRestore(req.Name); errors.Is(err, tasks.ErrBackupNotFound) {
		return RestoreBackup404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "restoreBackup", err)
	}
	h.restartSoon()
	return RestoreBackup202Response{}, nil
}

func (h *Handlers) RestartServer(ctx context.Context, _ RestartServerRequestObject) (RestartServerResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RestartServer401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RestartServer403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	h.restartSoon()
	return RestartServer202Response{}, nil
}

// restartSoon restarts after the response has been sent.
func (h *Handlers) restartSoon() {
	if h.Restart != nil {
		time.AfterFunc(500*time.Millisecond, h.Restart)
	}
}
