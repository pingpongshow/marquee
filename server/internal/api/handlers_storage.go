package api

import "context"

// StorageReport is the storage overview (Dashboard → Storage), admins only.
func (h *Handlers) StorageReport(ctx context.Context, req StorageReportRequestObject) (StorageReportResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return StorageReport401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return StorageReport403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	return StorageReport200JSONResponse{Libraries: []StorageLibrary{}, Largest: []StorageTitle{}, Volumes: []StorageVolume{}}, nil
}
