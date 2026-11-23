package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"marquee/internal/images"
	"marquee/internal/items"
	"marquee/internal/metadata"
	"marquee/internal/metadata/tmdb"
	"marquee/internal/scanner"
)

func (h *Handlers) EditItem(ctx context.Context, req EditItemRequestObject) (EditItemResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return EditItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return EditItem403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	set := map[string]any{}
	str := func(name string, v *string) {
		if v != nil {
			set[name] = strings.TrimSpace(*v)
		}
	}
	str("title", b.Title)
	str("sortTitle", b.SortTitle)
	str("originalTitle", b.OriginalTitle)
	str("summary", b.Summary)
	str("tagline", b.Tagline)
	str("contentRating", b.ContentRating)
	str("studio", b.Studio)
	str("artistCredit", b.ArtistCredit)
	if b.Index != nil {
		set["index"] = *b.Index
	}
	tags := map[string][]string{}
	for name, list := range map[string]*[]string{"genres": b.Genres, "styles": b.Styles, "moods": b.Moods, "countries": b.Countries} {
		if list != nil {
			tags[name] = *list
		}
	}
	if t, ok := set["title"].(string); ok && t == "" {
		return EditItem400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "title cannot be empty"))}, nil
	}
	if b.Year != nil {
		set["year"] = *b.Year
	}
	if b.OriginallyAvailableAt != nil {
		set["originallyAvailableAt"] = b.OriginallyAvailableAt.Format("2006-01-02")
		if _, given := set["year"]; !given {
			set["year"] = b.OriginallyAvailableAt.Year()
		}
	}
	var unlock []string
	if b.Unlock != nil {
		for _, u := range *b.Unlock {
			unlock = append(unlock, string(u))
		}
	}
	if err := h.Items.Edit(ctx, req.ItemId, set, tags, unlock); errors.Is(err, items.ErrNotFound) {
		return EditItem404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "editItem", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "editItem", err)
	}
	return EditItem200JSONResponse(out), nil
}

// metaError maps metadata errors to a user-facing message; ok=false means internal error.
func metaError(err error) (string, bool) {
	var ref metadata.MatchRefError
	switch {
	case errors.As(err, &ref):
		return ref.Error(), true
	case errors.Is(err, metadata.ErrNotMusicMatchable):
		return err.Error(), true
	case errors.Is(err, metadata.ErrNotMatchable), errors.Is(err, metadata.ErrNotMatched):
		return err.Error(), true
	case errors.Is(err, tmdb.ErrNoKey):
		return "add a TMDB API key in Settings → Metadata first", true
	case errors.Is(err, tmdb.ErrInvalidKey):
		return err.Error(), true
	case errors.Is(err, tmdb.ErrNotFound):
		return "that entry no longer exists on TMDB", true
	}
	return "", false
}

func (h *Handlers) SearchMatches(ctx context.Context, req SearchMatchesRequestObject) (SearchMatchesResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return SearchMatches401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SearchMatches403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	title, year := "", 0
	set(&title, req.Params.Title)
	set(&year, req.Params.Year)
	cands, err := h.Metadata.Candidates(ctx, req.ItemId, strings.TrimSpace(title), year)
	if err != nil {
		if items.IsNotFound(err) {
			return SearchMatches404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return SearchMatches400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "searchMatches", err)
	}
	out := make(SearchMatches200JSONResponse, len(cands))
	for i, c := range cands {
		out[i] = MatchCandidate{Provider: MatchCandidateProvider(c.Provider), Id: c.ID, Title: c.Title, OriginalTitle: nz(c.Original),
			Year: nz(c.Year), Overview: nz(c.Overview), PosterUrl: nz(c.PosterURL), Current: ptr(c.Current)}
	}
	return out, nil
}

func (h *Handlers) ApplyMatch(ctx context.Context, req ApplyMatchRequestObject) (ApplyMatchResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ApplyMatch401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ApplyMatch403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	id, err := strconv.Atoi(req.Body.Id)
	if err != nil || id <= 0 {
		return ApplyMatch400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "invalid TMDB id"))}, nil
	}
	if err := h.Metadata.MatchTo(ctx, req.ItemId, id); err != nil {
		if items.IsNotFound(err) {
			return ApplyMatch404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return ApplyMatch400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "applyMatch", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "applyMatch", err)
	}
	return ApplyMatch200JSONResponse(out), nil
}

func (h *Handlers) SearchMusicMatches(ctx context.Context, req SearchMusicMatchesRequestObject) (SearchMusicMatchesResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return SearchMusicMatches401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SearchMusicMatches403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	q, artist := "", ""
	set(&q, req.Params.Q)
	set(&artist, req.Params.Artist)
	cands, err := h.Metadata.MusicCandidates(ctx, req.ItemId, q, artist)
	if err != nil {
		if items.IsNotFound(err) {
			return SearchMusicMatches404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return SearchMusicMatches400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "searchMusicMatches", err)
	}
	out := make(SearchMusicMatches200JSONResponse, len(cands))
	for i, c := range cands {
		out[i] = MusicMatchCandidate{Mbid: c.MBID, Name: c.Name, Disambiguation: nz(c.Disambiguation), Type: nz(c.Type), Country: nz(c.Country),
			Year: nz(c.Year), BeginYear: nz(c.BeginYear), EndYear: nz(c.EndYear), Artist: nz(c.Artist), TrackCount: nz(c.TrackCount),
			Score: nz(c.Score), Current: ptr(c.Current)}
	}
	return out, nil
}

func (h *Handlers) ApplyMusicMatch(ctx context.Context, req ApplyMusicMatchRequestObject) (ApplyMusicMatchResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ApplyMusicMatch401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ApplyMusicMatch403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Mbid) == "" {
		return ApplyMusicMatch400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "a MusicBrainz id is required"))}, nil
	}
	if err := h.Metadata.MusicMatchTo(ctx, req.ItemId, req.Body.Mbid); err != nil {
		if items.IsNotFound(err) {
			return ApplyMusicMatch404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return ApplyMusicMatch400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "applyMusicMatch", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "applyMusicMatch", err)
	}
	return ApplyMusicMatch200JSONResponse(out), nil
}

// placementError is the message for a track that can't be moved there.
func placementError(err error) (string, bool) {
	switch {
	case errors.Is(err, scanner.ErrNotTrack), errors.Is(err, scanner.ErrAlbumNotInLibrary), errors.Is(err, scanner.ErrNoAlbum):
		return err.Error(), true
	}
	return "", false
}

func (h *Handlers) SetTrackPlacement(ctx context.Context, req SetTrackPlacementRequestObject) (SetTrackPlacementResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return SetTrackPlacement401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SetTrackPlacement403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	var p scanner.Placement
	if req.Body != nil {
		set(&p.AlbumID, req.Body.AlbumId)
		set(&p.AlbumArtist, req.Body.AlbumArtist)
		set(&p.Album, req.Body.Album)
		set(&p.Artist, req.Body.Artist)
	}
	if err := scanner.PlaceTrack(ctx, h.DB, req.ItemId, p); err != nil {
		if items.IsNotFound(err) {
			return SetTrackPlacement404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := placementError(err); ok {
			return SetTrackPlacement400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
		}
		return nil, internal(ctx, "setTrackPlacement", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "setTrackPlacement", err)
	}
	return SetTrackPlacement200JSONResponse(out), nil
}

func (h *Handlers) ClearTrackPlacement(ctx context.Context, req ClearTrackPlacementRequestObject) (ClearTrackPlacementResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ClearTrackPlacement401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ClearTrackPlacement403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := scanner.UnplaceTrack(ctx, h.DB, req.ItemId); err != nil {
		if items.IsNotFound(err) {
			return ClearTrackPlacement404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := placementError(err); ok {
			return ClearTrackPlacement400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
		}
		return nil, internal(ctx, "clearTrackPlacement", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "clearTrackPlacement", err)
	}
	return ClearTrackPlacement200JSONResponse(out), nil
}

func (h *Handlers) RefreshItem(ctx context.Context, req RefreshItemRequestObject) (RefreshItemResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RefreshItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RefreshItem403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Metadata.Refresh(ctx, req.ItemId); err != nil {
		if items.IsNotFound(err) {
			return RefreshItem404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return RefreshItem400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "refresh", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "refresh", err)
	}
	return RefreshItem200JSONResponse(out), nil
}

// ---------- artwork (META-13) ----------

// nzSlice is nil for an empty list (left out of the JSON).
func nzSlice(v []string) *[]string {
	if len(v) == 0 {
		return nil
	}
	return &v
}

func (h *Handlers) ListItemArtwork(ctx context.Context, req ListItemArtworkRequestObject) (ListItemArtworkResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ListItemArtwork401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListItemArtwork403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	kind := string(req.Params.Kind)
	own, err := h.Items.ItemArtwork(ctx, req.ItemId, kind)
	if errors.Is(err, items.ErrNotFound) {
		return ListItemArtwork404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "itemArtwork", err)
	}
	out := ListItemArtwork200JSONResponse{}
	have := map[string]bool{}
	for _, a := range own {
		out = append(out, ArtworkOption{Id: ptr(a.ID), Source: a.Source, PreviewUrl: fmt.Sprintf("/api/v1/images/%d?w=360", a.ID),
			Width: nz(a.Width), Height: nz(a.Height), Language: nz(a.Language), Selected: a.Selected})
		if a.RemoteURL != "" {
			have[a.RemoteURL] = true
		}
	}
	// TMDB's other images: a failure there (no key, offline) leaves just the item's own.
	if h.Metadata != nil {
		alts, err := h.Metadata.ArtworkAlternatives(ctx, req.ItemId, kind)
		if err != nil {
			slog.WarnContext(ctx, "artwork alternatives", "item", req.ItemId, "err", err)
		}
		for _, a := range alts {
			if have[a.URL] {
				continue
			}
			out = append(out, ArtworkOption{Source: "tmdb", PreviewUrl: a.PreviewURL, Url: ptr(a.URL), Width: nz(a.Width), Height: nz(a.Height),
				Language: nz(a.Language), Votes: nz(float32(a.Votes))})
		}
	}
	return out, nil
}

func (h *Handlers) ChooseItemArtwork(ctx context.Context, req ChooseItemArtworkRequestObject) (ChooseItemArtworkResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ChooseItemArtwork401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ChooseItemArtwork403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b, kind := req.Body, string(req.Kind)
	var err error
	switch {
	case b.ArtworkId != nil:
		err = h.Items.ChooseArtwork(ctx, req.ItemId, kind, *b.ArtworkId)
	case b.Url != nil:
		u, perr := url.Parse(strings.TrimSpace(*b.Url))
		if perr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return ChooseItemArtwork400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "an image link starts with https://"))}, nil
		}
		// The link is fetched now, so one that isn't an image (a page about it, say) is
		// refused here rather than chosen and shown as a broken picture.
		var width, height int
		if _, aerr := h.Items.ItemArtwork(ctx, req.ItemId, kind); aerr != nil {
			err = aerr
			break
		}
		if h.Images != nil {
			width, height, err = h.Images.CheckChosen(ctx, kind, u.String())
			switch {
			case errors.Is(err, images.ErrPrivateAddress):
				return ChooseItemArtwork400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "that link leads to this server or your home network: use an image on the web, or upload it"))}, nil
			case errors.Is(err, images.ErrNotImage):
				return ChooseItemArtwork400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "that link isn't a JPEG, PNG or WebP image: use the image's own address"))}, nil
			case err != nil:
				slog.WarnContext(ctx, "artwork link", "item", req.ItemId, "err", err)
				return ChooseItemArtwork400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "the image couldn't be downloaded from that link"))}, nil
			}
		}
		err = h.Items.ChooseArtworkURL(ctx, req.ItemId, kind, u.String(), width, height)
	default:
		return ChooseItemArtwork400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "choose an image or give a link"))}, nil
	}
	if errors.Is(err, items.ErrNotFound) {
		return ChooseItemArtwork404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item or image not found"))}, nil
	}
	if errors.Is(err, items.ErrArtworkKind) {
		return ChooseItemArtwork400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "chooseArtwork", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "chooseArtwork", err)
	}
	return ChooseItemArtwork200JSONResponse(out), nil
}

const maxArtworkUpload = 20 << 20

func (h *Handlers) UploadItemArtwork(ctx context.Context, req UploadItemArtworkRequestObject) (UploadItemArtworkResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return UploadItemArtwork401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UploadItemArtwork403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	bad := func(msg string) (UploadItemArtworkResponseObject, error) {
		return UploadItemArtwork400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, maxArtworkUpload+1))
	if err != nil {
		return bad("couldn't read the image")
	}
	if len(data) > maxArtworkUpload {
		return bad("the image is larger than 20 MB")
	}
	ext := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}[http.DetectContentType(data)]
	if ext == "" {
		return bad("use a JPEG, PNG or WebP image")
	}
	// Its header must read too: a cut-short or damaged file would be chosen and never show.
	if _, _, err := images.CheckImage(data); err != nil {
		return bad("that image couldn't be read: use a JPEG, PNG or WebP image")
	}
	if _, err := h.Items.ItemArtwork(ctx, req.ItemId, string(req.Kind)); errors.Is(err, items.ErrNotFound) {
		return UploadItemArtwork404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	} else if err != nil {
		return bad(err.Error())
	}
	sum := sha256.Sum256(data)
	rel := filepath.Join("uploads", fmt.Sprintf("%d-%s-%x.%s", req.ItemId, req.Kind, sum[:8], ext))
	abs := filepath.Join(h.Images.CacheDir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, internal(ctx, "uploadArtwork", err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return nil, internal(ctx, "uploadArtwork", err)
	}
	if err := h.Items.AddUploadedArtwork(ctx, req.ItemId, string(req.Kind), rel); err != nil {
		return nil, internal(ctx, "uploadArtwork", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "uploadArtwork", err)
	}
	return UploadItemArtwork200JSONResponse(out), nil
}

func (h *Handlers) ResetItemArtwork(ctx context.Context, req ResetItemArtworkRequestObject) (ResetItemArtworkResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ResetItemArtwork401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ResetItemArtwork403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	uploads, err := h.Items.ResetArtwork(ctx, req.ItemId, string(req.Kind))
	if errors.Is(err, items.ErrNotFound) {
		return ResetItemArtwork404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "resetArtwork", err)
	}
	for _, p := range uploads {
		if !filepath.IsAbs(p) && !strings.HasPrefix(filepath.Clean(p), "..") {
			os.Remove(filepath.Join(h.Images.CacheDir, p))
		}
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "resetArtwork", err)
	}
	return ResetItemArtwork200JSONResponse(out), nil
}
