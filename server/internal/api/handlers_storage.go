package api

import (
	"context"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"marquee/internal/storage"
)

// StorageReport is the storage overview (Dashboard → Storage), admins only.
func (h *Handlers) StorageReport(ctx context.Context, req StorageReportRequestObject) (StorageReportResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return StorageReport401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return StorageReport403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	svc := h.Storage
	if svc == nil {
		svc = &storage.Service{DB: h.DB}
	}
	days, top := 90, 20
	if p := req.Params.Days; p != nil {
		days = min(max(*p, 7), 365)
	}
	if p := req.Params.Top; p != nil {
		top = min(max(*p, 1), 100)
	}
	libs, err := svc.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	hist, err := svc.History(ctx, days)
	if err != nil {
		return nil, err
	}
	largest, err := svc.Largest(ctx, top)
	if err != nil {
		return nil, err
	}
	out := StorageReport200JSONResponse{Libraries: []StorageLibrary{}, Largest: []StorageTitle{}, Volumes: []StorageVolume{}}
	for _, l := range libs {
		samples := []StorageSample{}
		for _, x := range hist[l.ID] {
			if d, err := time.Parse(time.DateOnly, x.Date); err == nil {
				samples = append(samples, StorageSample{Date: openapi_types.Date{Time: d}, Bytes: x.Bytes})
			}
		}
		name := l.Name
		if l.Recordings && !strings.Contains(strings.ToLower(name), "record") {
			name += " (recordings)"
		}
		out.Libraries = append(out.Libraries, StorageLibrary{Id: l.ID, Name: name, Type: LibraryType(l.Type),
			Bytes: l.Bytes, Files: l.Files, History: samples})
	}
	for _, t := range largest {
		st := StorageTitle{ItemId: t.ItemID, LibraryId: t.LibraryID, Title: t.Title, Type: t.Type, Bytes: t.Bytes, Files: ptr(t.Files)}
		if t.Year > 0 {
			st.Year = ptr(t.Year)
		}
		out.Largest = append(out.Largest, st)
	}
	for _, v := range svc.Volumes(ctx) {
		sv := StorageVolume{Path: v.Path, TotalBytes: v.TotalBytes, FreeBytes: v.FreeBytes}
		if v.Label != "" {
			sv.Label = ptr(v.Label)
		}
		out.Volumes = append(out.Volumes, sv)
	}
	return out, nil
}
