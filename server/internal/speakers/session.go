package speakers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// playedPercent is how far a song must get to count as played, as in playback.Progress.
	playedPercent = 50
	// restartAfter is how far in "previous" restarts the song instead of going back, as the
	// apps' own players do.
	restartAfter = 3000
	// endSlack is how near the end a song must have been seen for a stopped speaker to mean it
	// finished, rather than someone pressing stop in the Sonos app.
	endSlack = 6000
)

// session is what Marquee is playing in a room. It's guarded by mu, held across calls to the
// speaker: they have short timeouts, and only this room's callers wait. The speaker itself must
// never wait on that, as it may fetch a song before answering the call that told it to, so what
// the stream routes need has dataMu as well.
type session struct {
	svc         *Service
	token       string // in the URLs given to the speaker; never anyone's sign-in token
	coordinator string
	owner       int64
	label       string // statistics context
	dir         string
	ctx         context.Context
	cancel      context.CancelFunc
	wake        chan struct{}

	mu sync.Mutex
	// dataMu is also held, briefly and never across a call to the speaker, to change queue,
	// files or ended; so they can be read with either lock.
	dataMu   sync.Mutex
	queue    []Track
	index    int
	pending  bool  // queue[index] is still to be given to the speaker, when it's ready
	startMS  int64 // where to start it
	paused   bool  // pending, and paused meanwhile: it waits for a resume once it's ready
	silenced bool  // the speaker was stopped while the next song is prepared
	nextSet  int   // the queue index the speaker was told comes next; -1 = none
	noNext   bool  // the speaker refused SetNextAVTransportURI: we move on at each song's end
	files    map[int64]*prepared
	grace    time.Time // until then, the speaker is still taking in what it was told
	state    string
	posMS    int64
	durMS    int64
	failedAt time.Time // when it first stopped answering; zero while it answers
	ended    bool

	// The song's play, once it has passed playedPercent.
	counted    bool
	historyID  int64
	trackStart time.Time
}

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) newSession(r Room, owner int64, tracks []Track, start int, startMS int64, label string) *session {
	s.mu.Lock()
	base := s.base
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(base)
	ss := &session{svc: s, token: newToken(), coordinator: r.Coordinator, owner: owner, label: label, ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, 1), queue: slices.Clone(tracks), index: min(max(start, 0), len(tracks)-1), pending: true,
		startMS: max(startMS, 0), nextSet: -1, files: map[int64]*prepared{}, state: "loading", trackStart: time.Now()}
	ss.dir = filepath.Join(s.Dir, ss.token)
	s.mu.Lock()
	s.sessions[r.Coordinator] = ss
	s.tokens[ss.token] = ss
	s.mu.Unlock()
	return ss
}

// run polls the speaker until the session ends.
func (ss *session) run() {
	t := time.NewTicker(ss.svc.PollEvery)
	defer t.Stop()
	for {
		select {
		case <-ss.ctx.Done():
			return
		case <-t.C:
		case <-ss.wake:
		}
		ss.mu.Lock()
		ss.tick(ss.ctx)
		ended := ss.ended
		ss.mu.Unlock()
		if ended {
			return
		}
	}
}

func (ss *session) cur() Track { return ss.queue[ss.index] }

// finish ends the session. It tells the speaker nothing: whoever is ending it decides that.
func (ss *session) finish(why string) {
	if ss.ended {
		return
	}
	ss.dataMu.Lock()
	ss.ended = true
	ss.dataMu.Unlock()
	ss.closeTrack(false)
	ss.cancel()
	if err := os.RemoveAll(ss.dir); err != nil {
		slog.Warn("speakers: remove conversions", "err", err)
	}
	s := ss.svc
	s.mu.Lock()
	if s.sessions[ss.coordinator] == ss {
		delete(s.sessions, ss.coordinator)
	}
	delete(s.tokens, ss.token)
	s.mu.Unlock()
	slog.Info("speakers: session ended", "why", why)
}

func (ss *session) fill(st *Status) {
	st.Marquee = true
	st.State = ss.state
	st.ItemID = ss.cur().ItemID
	st.PositionMS, st.DurationMS = ss.posMS, ss.durMS
	if ss.pending {
		st.State, st.PositionMS, st.DurationMS = "loading", ss.startMS, ss.cur().DurationMS
		if ss.paused {
			st.State = "paused"
		}
	}
	st.QueueIndex, st.QueueLength = ss.index, len(ss.queue)
}

// tick is one poll: it follows the speaker along the queue, counts plays, gives it the next
// song, and ends the session when the queue is over or another app has the room.
func (ss *session) tick(ctx context.Context) {
	if ss.ended {
		return
	}
	if ss.pending {
		if err := ss.startIfReady(ctx); err != nil {
			ss.failed(err)
		} else if ss.paused {
			ss.checkWhilePaused(ctx)
		}
		return
	}
	addr, ok := ss.svc.addrOf(ss.coordinator)
	if !ok {
		ss.failed(ErrUnreachable)
		return
	}
	state, err := ss.svc.transportInfo(ctx, addr)
	if err != nil {
		ss.failed(err)
		return
	}
	pos, err := ss.svc.positionInfo(ctx, addr)
	if err != nil {
		ss.failed(err)
		return
	}
	ss.failedAt = time.Time{}
	settling := time.Now().Before(ss.grace)
	ss.state = apiState(state)

	item, ours := ss.parseStreamURI(pos.URI)
	if !ours {
		if !settling {
			ss.finish("another app took over the room")
		}
		return
	}
	if item != ss.cur().ItemID {
		switch i := slices.IndexFunc(ss.queue, func(t Track) bool { return t.ItemID == item }); {
		case ss.nextSet >= 0 && ss.queue[ss.nextSet].ItemID == item:
			ss.moved(ss.nextSet, ss.nearEnd())
		case settling:
			return // still on the song it's just been moved from
		case i >= 0:
			ss.moved(i, false)
		default:
			ss.finish("the speaker is on a song that isn't in the queue")
			return
		}
		settling = time.Now().Before(ss.grace)
	}

	switch state {
	case "PLAYING", "PAUSED_PLAYBACK":
		ss.grace = time.Time{}
		ss.posMS = pos.RelMS
		if pos.DurationMS > 0 {
			ss.durMS = pos.DurationMS
		}
		ss.countIfPlayed(ctx)
		ss.ensureNext(ctx, addr)
	case "TRANSITIONING":
	default: // STOPPED, NO_MEDIA_PRESENT
		if settling {
			ss.state = "loading"
			return
		}
		ss.stopped(ctx)
	}
}

// nearEnd reports the song having last been seen about to finish.
func (ss *session) nearEnd() bool {
	return ss.durMS > 0 && ss.posMS >= ss.durMS-endSlack
}

// checkWhilePaused looks for another app using the room while a song waits for a resume, which
// could be a long time: the speaker has nothing of ours, so anything playing is theirs.
func (ss *session) checkWhilePaused(ctx context.Context) {
	addr, ok := ss.svc.addrOf(ss.coordinator)
	if !ok {
		ss.failed(ErrUnreachable)
		return
	}
	state, err := ss.svc.transportInfo(ctx, addr)
	if err != nil {
		ss.failed(err)
		return
	}
	ss.failedAt = time.Time{}
	if state == "PLAYING" {
		ss.finish("another app took over the room")
	}
}

// stopped decides what a speaker that has stopped by itself means. Near the end of a song it
// finished, and the speaker didn't move on (it wasn't told what's next in time).
func (ss *session) stopped(ctx context.Context) {
	if !ss.nearEnd() {
		ss.finish("stopped outside Marquee")
		return
	}
	ss.closeTrack(true)
	if ss.index+1 >= len(ss.queue) {
		ss.finish("the queue is over")
		return
	}
	ss.index++
	ss.pending, ss.startMS, ss.nextSet = true, 0, -1
	ss.evict()
	if err := ss.startIfReady(ctx); err != nil {
		ss.failed(err)
	}
}

// failed notes the speaker not answering, and gives up on it after a while. Its address may
// have changed, so the rooms are read again meanwhile.
func (ss *session) failed(err error) {
	slog.Debug("speakers: poll", "err", err)
	if ss.failedAt.IsZero() {
		ss.failedAt = time.Now()
		go ss.svc.refresh(ss.ctx)
		return
	}
	if time.Since(ss.failedAt) >= giveUpAfter {
		ss.finish("the speaker stopped answering")
	}
}

// parseStreamURI finds which song of this session a speaker's track URI is.
func (ss *session) parseStreamURI(uri string) (int64, bool) {
	_, rest, ok := strings.Cut(uri, streamPrefix+ss.token+"/")
	if !ok {
		return 0, false
	}
	stem, _, _ := strings.Cut(rest, ".")
	id, err := strconv.ParseInt(stem, 10, 64)
	return id, err == nil
}

// startIfReady gives the song at index to the speaker once it's ready to be fetched. While a
// conversion runs the speaker is kept quiet, and a song that can't be prepared is skipped.
func (ss *session) startIfReady(ctx context.Context) error {
	for ss.index < len(ss.queue) {
		t := ss.cur()
		p := ss.prepare(t)
		select {
		case <-p.done:
		default:
			ss.state = "loading"
			if !ss.silenced {
				if addr, ok := ss.svc.addrOf(ss.coordinator); ok {
					if err := ignoreFault(ss.svc.stop(ctx, addr)); err == nil {
						ss.silenced = true
					}
				}
			}
			return nil
		}
		if p.err != nil {
			slog.Warn("speakers: skipping a song", "item", t.ItemID, "err", p.err)
			ss.skipCurrent()
			continue
		}
		if ss.paused {
			return nil
		}
		err := ss.start(ctx, t, p)
		var f *faultError
		if errors.As(err, &f) {
			slog.Warn("speakers: skipping a song the speaker refused", "item", t.ItemID, "err", err)
			ss.skipCurrent()
			continue
		}
		return err
	}
	ss.finish("nothing left to play")
	return nil
}

func (ss *session) dropEntry(i int) {
	ss.dataMu.Lock()
	defer ss.dataMu.Unlock()
	ss.queue = slices.Delete(ss.queue, i, i+1)
}

// skipCurrent drops the song at index. The place to start was in that song, not the next.
func (ss *session) skipCurrent() {
	ss.dropEntry(ss.index)
	ss.startMS = 0
}

// start tells the speaker to play a prepared song.
func (ss *session) start(ctx context.Context, t Track, p *prepared) error {
	addr, ok := ss.svc.addrOf(ss.coordinator)
	if !ok {
		return ErrUnreachable
	}
	uri, meta, err := ss.describe(addr, t, p)
	if err != nil {
		return err
	}
	if err := ss.svc.setURI(ctx, addr, uri, meta); err != nil {
		return err
	}
	if ss.startMS > 0 {
		// A speaker that won't seek before playing starts from the top: not worth failing for.
		if err := ss.svc.seekTo(ctx, addr, ss.startMS); err != nil {
			slog.Debug("speakers: seek before play", "err", err)
		}
	}
	if err := ss.svc.play(ctx, addr); err != nil {
		return err
	}
	ss.resetTrack(ss.startMS)
	ss.pending, ss.startMS, ss.silenced, ss.nextSet = false, 0, false, -1
	ss.state = "loading"
	ss.prepareAhead()
	return nil
}

// resetTrack starts following the song at index from the beginning of its play.
func (ss *session) resetTrack(posMS int64) {
	ss.counted, ss.historyID, ss.trackStart = false, 0, time.Now()
	ss.posMS, ss.durMS = posMS, ss.cur().DurationMS
	ss.grace = time.Now().Add(*ss.svc.Grace)
}

// moved notes the speaker being on queue index i already; finishedIt means it got there by
// playing the song before to its end.
func (ss *session) moved(i int, finishedIt bool) {
	slog.Debug("speakers: the speaker moved on", "index", i, "finishedIt", finishedIt)
	ss.closeTrack(finishedIt)
	ss.index, ss.nextSet = i, -1
	ss.resetTrack(0)
	ss.evict()
	ss.prepareAhead()
}

func (ss *session) prepareAhead() {
	if i := ss.index + 1; i < len(ss.queue) {
		ss.prepare(ss.queue[i])
	}
}

// ensureNext tells the speaker what comes after this song, for a gapless move, as soon as
// it's ready. A song repeated straight after itself would have the same URL, and couldn't be
// told apart from the speaker staying put, so that one is started by hand when this one ends.
func (ss *session) ensureNext(ctx context.Context, addr string) {
	if ss.nextSet >= 0 || ss.noNext {
		return
	}
	for i := ss.index + 1; i < len(ss.queue); {
		t := ss.queue[i]
		p := ss.prepare(t)
		select {
		case <-p.done:
		default:
			return
		}
		if p.err != nil {
			slog.Warn("speakers: skipping a song", "item", t.ItemID, "err", p.err)
			ss.dropEntry(i)
			continue
		}
		if t.ItemID == ss.cur().ItemID {
			return
		}
		uri, meta, err := ss.describe(addr, t, p)
		if err != nil {
			return
		}
		err = ss.svc.setNextURI(ctx, addr, uri, meta)
		var f *faultError
		if errors.As(err, &f) {
			slog.Info("speakers: no gapless moves on this speaker", "err", err)
			ss.noNext = true
			return
		}
		if err == nil {
			ss.nextSet = i
		}
		return
	}
}

// describe is the URL and metadata for a song, using the server's address on the route to addr.
func (ss *session) describe(addr string, t Track, p *prepared) (uri, meta string, err error) {
	base, err := ss.svc.localBase(addr)
	if err != nil {
		return "", "", err
	}
	id := strconv.FormatInt(t.ItemID, 10)
	uri = base + streamPrefix + ss.token + "/" + id + p.ext
	art := ""
	if t.ArtworkID != 0 {
		art = base + artPrefix + ss.token + "/" + id + ".jpg"
	}
	return uri, didl(t, uri, p.mime, art), nil
}

// goTo plays another song of the queue.
func (ss *session) goTo(ctx context.Context, i int) error {
	slog.Debug("speakers: going to a song", "index", i)
	ss.closeTrack(false)
	ss.index, ss.pending, ss.startMS, ss.nextSet = i, true, 0, -1
	ss.silenced, ss.paused = false, false
	ss.evict()
	return ss.startIfReady(ctx)
}

func (ss *session) next(ctx context.Context) error {
	addr, ok := ss.svc.addrOf(ss.coordinator)
	if !ok {
		return ErrUnreachable
	}
	if ss.index+1 >= len(ss.queue) {
		if err := ignoreFault(ss.svc.stop(ctx, addr)); err != nil {
			return err
		}
		ss.finish("skipped past the end of the queue")
		return nil
	}
	if !ss.pending && ss.nextSet == ss.index+1 {
		err := ss.svc.next(ctx, addr)
		if err == nil {
			ss.moved(ss.nextSet, false)
			return nil
		}
		var f *faultError
		if !errors.As(err, &f) {
			return err
		}
	}
	return ss.goTo(ctx, ss.index+1)
}

func (ss *session) previous(ctx context.Context) error {
	if !ss.pending && (ss.posMS > restartAfter || ss.index == 0) {
		addr, ok := ss.svc.addrOf(ss.coordinator)
		if !ok {
			return ErrUnreachable
		}
		if err := ss.svc.seekTo(ctx, addr, 0); err != nil {
			return err
		}
		ss.closeTrack(false)
		ss.resetTrack(0)
		return nil
	}
	if ss.index == 0 {
		ss.startMS = 0
		return nil
	}
	return ss.goTo(ctx, ss.index-1)
}

func (ss *session) seek(ctx context.Context, positionMS int64) error {
	if ss.pending {
		ss.startMS = positionMS
		return nil
	}
	if ss.durMS > 0 {
		positionMS = min(positionMS, max(ss.durMS-1000, 0))
	}
	addr, ok := ss.svc.addrOf(ss.coordinator)
	if !ok {
		return ErrUnreachable
	}
	if err := ss.svc.seekTo(ctx, addr, positionMS); err != nil {
		return err
	}
	ss.posMS = positionMS
	return nil
}

func (ss *session) add(tracks []Track) {
	ss.dataMu.Lock()
	defer ss.dataMu.Unlock()
	if over := len(ss.queue) + len(tracks) - MaxQueue; over > 0 {
		n := min(over, ss.index)
		ss.queue = slices.Delete(ss.queue, 0, n)
		ss.index -= n
		if ss.nextSet >= 0 {
			ss.nextSet -= n
		}
		tracks = tracks[:min(len(tracks), MaxQueue-len(ss.queue))]
	}
	ss.queue = append(ss.queue, tracks...)
}

// countIfPlayed counts the song in the history of whoever asked for it the first time it passes playedPercent.
func (ss *session) countIfPlayed(ctx context.Context) {
	if !ss.counted && ss.durMS > 0 && ss.posMS*100 >= ss.durMS*playedPercent {
		ss.count(ctx, ss.posMS)
	}
}

func (ss *session) count(ctx context.Context, posMS int64) {
	ss.counted = true
	t := ss.cur()
	who := t.AddedBy
	if who == 0 {
		who = ss.owner
	}
	ctx = context.WithoutCancel(ctx)
	const stamp = "2006-01-02T15:04:05.000Z"
	if _, err := ss.svc.DB.ExecContext(ctx, `INSERT INTO user_item_state(user_id, item_id, play_count, view_offset_ms, last_viewed_at)
		VALUES (?, ?, 1, 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		ON CONFLICT(user_id, item_id) DO UPDATE SET play_count = play_count + 1, view_offset_ms = 0,
		last_viewed_at = excluded.last_viewed_at, updated_at = excluded.last_viewed_at,
		watchlisted_at = NULL`, who, t.ItemID); err != nil {
		slog.Warn("speakers: count play", "err", err)
		return
	}
	title := t.Title
	if t.Artist != "" {
		title = t.Artist + " – " + t.Title
	}
	decision := "direct_play"
	if p := ss.files[t.ItemID]; p != nil && p.converted {
		decision = "transcode"
	}
	var label sql.NullString
	if ss.label != "" {
		label = sql.NullString{String: ss.label, Valid: true}
	}
	res, err := ss.svc.DB.ExecContext(ctx, `INSERT INTO play_history(user_id, item_id, item_title, started_at, stopped_at, position_ms,
		network_class, decision, source, context) VALUES (?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), ?, 'local', ?, 'sonos', ?)`,
		who, t.ItemID, title, ss.trackStart.UTC().Format(stamp), posMS, decision, label)
	if err != nil {
		slog.Warn("speakers: play history", "err", err)
		return
	}
	ss.historyID, _ = res.LastInsertId()
}

// closeTrack records how far the song got as it's left. finishedIt means it played out, which
// counts even if no poll happened to see it past half way.
func (ss *session) closeTrack(finishedIt bool) {
	ctx := context.WithoutCancel(ss.ctx)
	pos := ss.posMS
	if finishedIt {
		pos = max(ss.durMS, pos)
		if !ss.counted {
			ss.count(ctx, pos)
		}
	}
	if ss.historyID != 0 {
		if _, err := ss.svc.DB.ExecContext(ctx, `UPDATE play_history SET stopped_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), position_ms = ? WHERE id = ?`,
			pos, ss.historyID); err != nil {
			slog.Warn("speakers: play history", "err", err)
		}
	}
	ss.historyID = 0
}
