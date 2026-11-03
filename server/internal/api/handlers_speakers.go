package api

import (
	"context"
	"errors"

	"marquee/internal/items"
	"marquee/internal/netclass"
	"marquee/internal/speakers"
)

// Sonos rooms (MUSIC-26): the server plays to them itself, so these only ever tell it what to
// do. They're home speakers: signed in and at home only.

var errSpeakersAway = apiErr("forbidden", "Sonos rooms are only available at home")

// speakersAway reports a request from outside the home.
func speakersAway(ctx context.Context) bool { return requestInfo(ctx).Class == netclass.Remote }

func (h *Handlers) toAPISpeaker(ctx context.Context, st speakers.Status) Speaker {
	out := Speaker{Id: st.ID, Name: st.Name, Model: st.Model, State: SpeakerState(st.State), Volume: st.Volume, Marquee: st.Marquee}
	if st.PositionMS > 0 {
		out.PositionMs = ptr(st.PositionMS)
	}
	if st.DurationMS > 0 {
		out.DurationMs = ptr(st.DurationMS)
	}
	if !st.Marquee {
		return out
	}
	out.QueueIndex, out.QueueLength = ptr(st.QueueIndex), ptr(st.QueueLength)
	// Who's asking may not be who's listening: they see the song only if they could open it.
	if list, err := h.Items.ByIDs(ctx, access(ctx), []int64{st.ItemID}); err == nil && len(list) == 1 {
		out.Current = ptr(toAPISummary(list[0]))
	}
	return out
}

// speakerTrack is what a speaker needs of a song.
func speakerTrack(it items.Summary, by int64) speakers.Track {
	artist := it.ArtistCredit
	if artist == "" {
		artist = it.GrandparentTitle
	}
	art := it.Poster
	if art == 0 {
		art = it.Thumb
	}
	return speakers.Track{ItemID: it.ID, AddedBy: by, Title: it.Title, Artist: artist, Album: it.ParentTitle, DurationMS: it.DurationMS, ArtworkID: art}
}

// speakerTracks expands ids as Play buttons do (an album or artist into its songs), with what
// the caller may see, songs only. Their thumbs-down songs stay out, except that one song named
// directly: the entry at named, when it's that song itself. It returns where in the songs the
// entry at start begins, and never more than speakers.MaxQueue songs, from there if it must.
func (h *Handlers) speakerTracks(ctx context.Context, ids []int64, start, named int) ([]speakers.Track, int, error) {
	acc := access(ctx)
	disliked, err := h.Items.DislikedSongs(ctx, acc.UserID)
	if err != nil {
		return nil, 0, err
	}
	visible, err := h.Items.ByIDs(ctx, acc, ids)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[int64]items.Summary, len(visible))
	for _, it := range visible {
		byID[it.ID] = it
	}
	out := []speakers.Track{}
	first := 0
	for i, id := range ids {
		if i == start {
			first = len(out)
		}
		it, ok := byID[id]
		if !ok {
			continue
		}
		leaves := []items.Summary{it}
		if it.Type != "track" {
			if leaves, err = h.Items.Leaves(ctx, acc, id, false); err != nil {
				if errors.Is(err, items.ErrNotFound) {
					continue
				}
				return nil, 0, err
			}
		}
		for _, l := range leaves {
			if l.Type != "track" || !l.Available || l.ExtraType != "" {
				continue
			}
			if disliked[l.ID] && (i != named || l.ID != id) {
				continue
			}
			out = append(out, speakerTrack(l, acc.UserID))
		}
		if i >= start && len(out)-first >= speakers.MaxQueue {
			break
		}
	}
	if len(out) > speakers.MaxQueue {
		from := min(first, len(out)-speakers.MaxQueue)
		out, first = out[from:from+speakers.MaxQueue], first-from
	}
	return out, min(first, max(len(out)-1, 0)), nil
}

func (h *Handlers) ListSpeakers(ctx context.Context, _ ListSpeakersRequestObject) (ListSpeakersResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListSpeakers401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if speakersAway(ctx) {
		return ListSpeakers403JSONResponse{ForbiddenJSONResponse(errSpeakersAway)}, nil
	}
	out := []Speaker{}
	if h.Speakers != nil {
		for _, st := range h.Speakers.List(ctx) {
			out = append(out, h.toAPISpeaker(ctx, st))
		}
	}
	return ListSpeakers200JSONResponse(out), nil
}

var errNoSpeaker = apiErr("not_found", speakers.ErrNotFound.Error())

func (h *Handlers) GetSpeaker(ctx context.Context, req GetSpeakerRequestObject) (GetSpeakerResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetSpeaker401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if speakersAway(ctx) {
		return GetSpeaker403JSONResponse{ForbiddenJSONResponse(errSpeakersAway)}, nil
	}
	if h.Speakers == nil {
		return GetSpeaker404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	}
	st, err := h.Speakers.Get(ctx, req.SpeakerId)
	switch {
	case errors.Is(err, speakers.ErrNotFound):
		return GetSpeaker404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	case err != nil:
		return GetSpeaker503JSONResponse{ServiceUnavailableJSONResponse(apiErr("unavailable", err.Error()))}, nil
	}
	return GetSpeaker200JSONResponse(h.toAPISpeaker(ctx, st)), nil
}

func (h *Handlers) PlayOnSpeaker(ctx context.Context, req PlayOnSpeakerRequestObject) (PlayOnSpeakerResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return PlayOnSpeaker401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if speakersAway(ctx) {
		return PlayOnSpeaker403JSONResponse{ForbiddenJSONResponse(errSpeakersAway)}, nil
	}
	b := req.Body
	bad := func(msg string) (PlayOnSpeakerResponseObject, error) {
		return PlayOnSpeaker400JSONResponse{BadRequestJSONResponse(apiErr("bad_request", msg))}, nil
	}
	index, startMS, label := 0, int64(0), ""
	if b.Index != nil {
		index = *b.Index
	}
	if b.StartMs != nil {
		startMS = *b.StartMs
	}
	if b.Context != nil {
		label = playContext(*b.Context)
	}
	switch {
	case len(b.ItemIds) == 0:
		return bad("play needs itemIds")
	case len(b.ItemIds) > speakers.MaxQueue:
		return bad("play takes at most 2,000 items")
	case index < 0 || index >= len(b.ItemIds):
		return bad("index is outside itemIds")
	case startMS < 0:
		return bad("startMs cannot be negative")
	}
	if h.Speakers == nil {
		return PlayOnSpeaker404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	}
	tracks, start, err := h.speakerTracks(ctx, b.ItemIds, index, index)
	if err != nil {
		return nil, internal(ctx, "playOnSpeaker", err)
	}
	st, err := h.Speakers.Play(ctx, req.SpeakerId, s.User.ID, tracks, start, startMS, label)
	switch {
	case errors.Is(err, speakers.ErrNotFound):
		return PlayOnSpeaker404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	case errors.Is(err, speakers.ErrNothingToPlay):
		return PlayOnSpeaker404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "none of these can be played"))}, nil
	case err != nil:
		return PlayOnSpeaker503JSONResponse{ServiceUnavailableJSONResponse(apiErr("unavailable", err.Error()))}, nil
	}
	return PlayOnSpeaker200JSONResponse(h.toAPISpeaker(ctx, st)), nil
}

func (h *Handlers) AddToSpeakerQueue(ctx context.Context, req AddToSpeakerQueueRequestObject) (AddToSpeakerQueueResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return AddToSpeakerQueue401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if speakersAway(ctx) {
		return AddToSpeakerQueue403JSONResponse{ForbiddenJSONResponse(errSpeakersAway)}, nil
	}
	if h.Speakers == nil {
		return AddToSpeakerQueue404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	}
	ids := req.Body.ItemIds
	if len(ids) > speakers.MaxQueue {
		ids = ids[:speakers.MaxQueue]
	}
	named := -1
	if len(ids) == 1 {
		named = 0 // the one song someone asked for by name
	}
	tracks, _, err := h.speakerTracks(ctx, ids, 0, named)
	if err != nil {
		return nil, internal(ctx, "addToSpeakerQueue", err)
	}
	if len(tracks) == 0 {
		return AddToSpeakerQueue404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "none of these can be played"))}, nil
	}
	st, err := h.Speakers.Add(ctx, req.SpeakerId, tracks)
	switch {
	case errors.Is(err, speakers.ErrNotFound):
		return AddToSpeakerQueue404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	case errors.Is(err, speakers.ErrNotOurs):
		return AddToSpeakerQueue409JSONResponse{ConflictJSONResponse(apiErr("not_marquee", err.Error()))}, nil
	case err != nil:
		// This operation has no 503: a room that stopped answering has lost its session anyway.
		return AddToSpeakerQueue409JSONResponse{ConflictJSONResponse(apiErr("unavailable", err.Error()))}, nil
	}
	return AddToSpeakerQueue200JSONResponse(h.toAPISpeaker(ctx, st)), nil
}

func (h *Handlers) ControlSpeaker(ctx context.Context, req ControlSpeakerRequestObject) (ControlSpeakerResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ControlSpeaker401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if speakersAway(ctx) {
		return ControlSpeaker403JSONResponse{ForbiddenJSONResponse(errSpeakersAway)}, nil
	}
	b := req.Body
	bad := func(msg string) (ControlSpeakerResponseObject, error) {
		return ControlSpeaker400JSONResponse{BadRequestJSONResponse(apiErr("bad_request", msg))}, nil
	}
	if !b.Action.Valid() {
		return bad("unknown action")
	}
	if b.Action == ControlSpeakerJSONBodyActionSeek && (b.PositionMs == nil || *b.PositionMs < 0) {
		return bad("seek needs positionMs")
	}
	if b.Action == ControlSpeakerJSONBodyActionSetVolume && (b.Volume == nil || *b.Volume < 0 || *b.Volume > 100) {
		return bad("setVolume needs a volume from 0 to 100")
	}
	if h.Speakers == nil {
		return ControlSpeaker404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	}
	var st speakers.Status
	var err error
	id := req.SpeakerId
	switch b.Action {
	case ControlSpeakerJSONBodyActionPause:
		st, err = h.Speakers.Pause(ctx, id)
	case ControlSpeakerJSONBodyActionResume:
		st, err = h.Speakers.Resume(ctx, id)
	case ControlSpeakerJSONBodyActionStop:
		st, err = h.Speakers.Stop(ctx, id)
	case ControlSpeakerJSONBodyActionNext:
		st, err = h.Speakers.Next(ctx, id)
	case ControlSpeakerJSONBodyActionPrevious:
		st, err = h.Speakers.Previous(ctx, id)
	case ControlSpeakerJSONBodyActionSeek:
		st, err = h.Speakers.Seek(ctx, id, *b.PositionMs)
	case ControlSpeakerJSONBodyActionSetVolume:
		st, err = h.Speakers.SetVolume(ctx, id, *b.Volume)
	}
	switch {
	case errors.Is(err, speakers.ErrNotFound):
		return ControlSpeaker404JSONResponse{NotFoundJSONResponse(errNoSpeaker)}, nil
	case errors.Is(err, speakers.ErrNotOurs), errors.Is(err, speakers.ErrRefused):
		return ControlSpeaker409JSONResponse{ConflictJSONResponse(apiErr("conflict", err.Error()))}, nil
	case err != nil:
		return ControlSpeaker503JSONResponse{ServiceUnavailableJSONResponse(apiErr("unavailable", err.Error()))}, nil
	}
	return ControlSpeaker200JSONResponse(h.toAPISpeaker(ctx, st)), nil
}
