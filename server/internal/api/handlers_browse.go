package api

import (
	"context"
	"errors"
	"math/rand/v2"

	"marquee/internal/items"
	"marquee/internal/library"
)

func summaries(list []items.Summary) []ItemSummary {
	out := make([]ItemSummary, len(list))
	for i, s := range list {
		out[i] = toAPISummary(s)
	}
	return out
}

func (h *Handlers) LibraryFilters(ctx context.Context, req LibraryFiltersRequestObject) (LibraryFiltersResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return LibraryFilters401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	lib, err := h.Libraries.Get(ctx, req.LibraryId)
	if errors.Is(err, library.ErrNotFound) || (err == nil && !canSeeLibrary(ctx, req.LibraryId)) {
		return LibraryFilters404JSONResponse{NotFoundJSONResponse(apiErr("not_found", library.ErrNotFound.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "filters", err)
	}
	typ := items.TopLevelType(string(lib.Type))
	if req.Params.Type != nil {
		typ = string(*req.Params.Type)
	}
	f, err := h.Items.Facets(ctx, access(ctx), lib.ID, typ)
	if err != nil {
		return nil, internal(ctx, "filters", err)
	}
	facets := func(in []items.Facet) []Facet {
		out := make([]Facet, len(in))
		for i, x := range in {
			out[i] = Facet{Value: x.Value, Count: x.Count}
		}
		return out
	}
	out := LibraryFilters{Genres: facets(f.Genres), Decades: facets(f.Decades), ContentRatings: facets(f.ContentRatings), Letters: []LetterOffset{}}
	for _, l := range f.Letters {
		out.Letters = append(out.Letters, LetterOffset{Letter: l.Letter, Offset: l.Offset})
	}
	return LibraryFilters200JSONResponse(out), nil
}

func (h *Handlers) GetPerson(ctx context.Context, req GetPersonRequestObject) (GetPersonResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetPerson401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	p, err := h.Items.Person(ctx, access(ctx), req.PersonId)
	if errors.Is(err, items.ErrNotFound) {
		return GetPerson404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "person", err)
	}
	out := PersonDetail{Id: p.ID, Name: p.Name, HasPhoto: p.HasPhoto, Credits: make([]PersonCredit, len(p.Credits))}
	for i, c := range p.Credits {
		out.Credits[i] = PersonCredit{Item: toAPISummary(c.Item), Role: c.Role, Character: nz(c.Character)}
	}
	return GetPerson200JSONResponse(out), nil
}

func (h *Handlers) RelatedItems(ctx context.Context, req RelatedItemsRequestObject) (RelatedItemsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return RelatedItems401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); errors.Is(err, items.ErrNotFound) {
		return RelatedItems404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "related", err)
	}
	list, err := h.Items.Related(ctx, access(ctx), req.ItemId, 20)
	if err != nil {
		return nil, internal(ctx, "related", err)
	}
	return RelatedItems200JSONResponse(summaries(list)), nil
}

func (h *Handlers) ItemLeaves(ctx context.Context, req ItemLeavesRequestObject) (ItemLeavesResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ItemLeaves401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); errors.Is(err, items.ErrNotFound) {
		return ItemLeaves404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "leaves", err)
	}
	unwatched := req.Params.Unwatched != nil && *req.Params.Unwatched
	list, err := h.Items.Leaves(ctx, access(ctx), req.ItemId, unwatched)
	if err != nil {
		return nil, internal(ctx, "leaves", err)
	}
	if req.Params.Shuffle != nil && *req.Params.Shuffle {
		rand.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
	}
	return ItemLeaves200JSONResponse(summaries(list)), nil
}
