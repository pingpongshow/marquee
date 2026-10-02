package api

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"marquee/internal/stats"
)

// GetStats reports playback statistics (ADM-4).
func (h *Handlers) GetStats(ctx context.Context, req GetStatsRequestObject) (GetStatsResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return GetStats401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	days, limit := 30, 10
	if req.Params.Days != nil {
		days = *req.Params.Days
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	user := sess.User.ID // people see their own statistics
	if sess.User.IsAdmin {
		user = 0
		if req.Params.UserId != nil {
			user = *req.Params.UserId
		}
	}
	r, err := stats.Build(ctx, h.DB, days, user, limit)
	if err != nil {
		return nil, internal(ctx, "stats", err)
	}
	list := func(cs []stats.Count) []StatsCount {
		out := make([]StatsCount, len(cs))
		for i, c := range cs {
			out[i] = StatsCount{Id: nz(c.ID), Title: c.Title, Subtitle: nz(c.Sub), Plays: c.Plays, Hours: float32(c.Hours)}
		}
		return out
	}
	out := Stats{
		Plays: r.Plays, Hours: float32(r.Hours), VideoHours: float32(r.VideoHrs), MusicHours: float32(r.MusicHours), Users: r.Users,
		Movies: list(r.Movies), Shows: list(r.Shows), Artists: list(r.Artists), Albums: list(r.Albums), Tracks: list(r.Tracks),
		People: list(r.People), Platforms: list(r.Platforms), Local: r.Local, Remote: r.Remote,
	}
	out.Methods.DirectPlay, out.Methods.DirectStream, out.Methods.Transcode = r.Methods["direct_play"], r.Methods["direct_stream"], r.Methods["transcode"]
	if !r.Since.IsZero() {
		out.Since = &r.Since
	}
	out.Days = make([]struct {
		Date  openapi_types.Date `json:"date"`
		Hours float32            `json:"hours"`
		Music int                `json:"music"`
		Video int                `json:"video"`
	}, 0, len(r.Days))
	for _, d := range r.Days {
		t, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		out.Days = append(out.Days, struct {
			Date  openapi_types.Date `json:"date"`
			Hours float32            `json:"hours"`
			Music int                `json:"music"`
			Video int                `json:"video"`
		}{openapi_types.Date{Time: t}, float32(d.Hours), d.Music, d.Video})
	}
	return GetStats200JSONResponse(out), nil
}
