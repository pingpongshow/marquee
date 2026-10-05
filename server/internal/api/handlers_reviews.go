package api

import (
	"context"
	"math"
	"unicode/utf8"

	"marquee/internal/auth"
)

// ---------- community ratings and comments (USER-17) ----------

func (h *Handlers) ItemReviews(ctx context.Context, req ItemReviewsRequestObject) (ItemReviewsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ItemReviews401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return ItemReviews404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	avg, count, list, err := h.Items.Reviews(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "reviews", err)
	}
	out := ItemReviews{Count: count, Reviews: []Review{}}
	if count > 0 {
		out.Average = ptr(float32(math.Round(avg*10) / 10))
	}
	for _, r := range list {
		rv := Review{UserId: r.UserID, UserName: r.UserName, UpdatedAt: r.UpdatedAt, Mine: r.UserID == s.User.ID,
			AvatarUrl: avatarURL(auth.User{ID: r.UserID, Avatar: r.Avatar}), Comment: nz(r.Comment)}
		if r.Rating > 0 {
			rv.Rating = ptr(float32(r.Rating))
		}
		out.Reviews = append(out.Reviews, rv)
	}
	return ItemReviews200JSONResponse(out), nil
}

func (h *Handlers) SetReview(ctx context.Context, req SetReviewRequestObject) (SetReviewResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SetReview401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return SetReview404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if utf8.RuneCountInString(req.Body.Comment) > 2000 {
		return SetReview400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "comments can be at most 2,000 characters"))}, nil
	}
	// A change made offline and sent later only applies if nothing newer happened (USER-18).
	if fresh, err := h.Items.Fresh(ctx, s.User.ID, req.ItemId, "comment", req.Body.At); err != nil {
		return nil, internal(ctx, "review", err)
	} else if !fresh {
		return SetReview204Response{}, nil
	}
	if err := h.Items.SetComment(ctx, s.User.ID, req.ItemId, req.Body.Comment); err != nil {
		return nil, internal(ctx, "review", err)
	}
	return SetReview204Response{}, nil
}

func (h *Handlers) DeleteReview(ctx context.Context, req DeleteReviewRequestObject) (DeleteReviewResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DeleteReview401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if req.UserId != s.User.ID && !s.User.IsAdmin {
		return DeleteReview403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return DeleteReview404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	// A change made offline and sent later only applies if nothing newer happened (USER-18).
	if fresh, err := h.Items.Fresh(ctx, req.UserId, req.ItemId, "comment", req.Params.At); err != nil {
		return nil, internal(ctx, "deleteReview", err)
	} else if !fresh {
		return DeleteReview204Response{}, nil
	}
	if err := h.Items.DeleteComment(ctx, req.UserId, req.ItemId); err != nil {
		return nil, internal(ctx, "deleteReview", err)
	}
	return DeleteReview204Response{}, nil
}
