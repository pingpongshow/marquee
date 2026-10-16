package plexsync

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"marquee/internal/settings"
)

const isoMS = "2006-01-02T15:04:05.000Z"

// syncItem decides and applies one (user, item) and moves its ledger on. A failure is
// counted and the run carries on; only Plex becoming unreachable (or rejecting the user's
// token) stops it.
func (s *Service) syncItem(ctx context.Context, srv *client, cfg settings.PlexSync, u *User, item int64, md Metadata, m mstate,
	l ledgerRow, lastRun time.Time, rep *Report, dry bool) error {
	pl := decide(cfg, m, md, l, lastRun, s.now().UTC())
	title := md.DisplayTitle()
	for _, c := range pl.changes {
		c.User, c.Title = u.Name, title
		rep.add(c)
		if c.Direction == "toPlex" {
			rep.ToPlex++
		} else {
			rep.ToMarquee++
		}
	}
	rep.Conflicts += pl.conflicts
	rep.Skipped += pl.skipped
	if pl.empty() {
		if pl.skipped == 0 {
			rep.Unchanged++
		}
		if !dry {
			if next, changed := moveLedger(l, pl, m, md, true, true, true); changed {
				return s.saveLedger(ctx, u.UserID, md.RatingKey, item, next)
			}
		}
		return nil
	}
	if dry {
		return nil
	}

	// Marquee first: it can't fail half way.
	mAfter := m
	if pl.setCount != nil || pl.setLastViewed != nil || pl.setOffset != nil || pl.setRating {
		if err := s.applyMarquee(ctx, u.UserID, item, pl); err != nil {
			rep.fail(fmt.Sprintf("%s, %s: %v", u.Name, title, err))
			return nil
		}
		if pl.setCount != nil {
			mAfter.count = *pl.setCount
			if pl.setOffset == nil {
				mAfter.offset = 0
			}
		}
		if pl.setOffset != nil {
			mAfter.offset = *pl.setOffset
		}
		if pl.setRating {
			mAfter.rating = pl.newRating
		}
	}

	// Then Plex.
	watchOK, offsetOK, ratingOK := true, true, true
	pAfter := md
	pushed := false
	var stop error
	failed := func(what string, err error) {
		if errors.Is(err, ErrUnreachable) || errors.Is(err, ErrUnauthorized) || ctx.Err() != nil {
			stop = err
			return
		}
		rep.fail(fmt.Sprintf("%s, %s: %s: %s", u.Name, title, what, srv.secrets.redact(err.Error())))
	}
	if pl.unscrobble {
		if err := srv.unscrobble(ctx, u.token, md.RatingKey); err != nil {
			watchOK = false
			failed("marking unplayed", err)
		} else {
			pushed = true
			pAfter.ViewCount, pAfter.ViewOffset = 0, 0
		}
	}
	for i := 0; i < pl.scrobbles && stop == nil; i++ {
		if err := srv.scrobble(ctx, u.token, md.RatingKey); err != nil {
			watchOK = false
			failed("marking played", err)
			break
		}
		pushed = true
		pAfter.ViewCount++
		pAfter.ViewOffset = 0
		s.DB.ExecContext(ctx, `INSERT INTO plex_sync_scrobbles(user_id, rating_key, pushed_at) VALUES (?, ?, ?)`, u.UserID, md.RatingKey, s.now().Unix())
	}
	if pl.pushOffset != nil && stop == nil {
		if err := srv.timeline(ctx, u.token, md.RatingKey, *pl.pushOffset, md.Duration); err != nil {
			offsetOK = false
			failed("setting the resume position", err)
		} else {
			pushed = true
			pAfter.ViewOffset = *pl.pushOffset
		}
	}
	if pl.pushRating && stop == nil {
		if err := srv.rate(ctx, u.token, md.RatingKey, pl.plexRating); err != nil {
			ratingOK = false
			failed("rating", err)
		} else {
			pushed = true
			pAfter.UserRating = pl.plexRating
		}
	}
	if stop != nil {
		return stop
	}
	// Read back what Plex now has, so the ledger holds Plex's own values and the next run
	// doesn't mistake our push for a change made in Plex.
	if pushed {
		if fresh, err := srv.metadata(ctx, u.token, md.RatingKey); err == nil {
			fresh.Type = md.Type
			pAfter = fresh
		} else if errors.Is(err, ErrUnreachable) {
			return err
		}
	}
	next, _ := moveLedger(l, pl, mAfter, pAfter, watchOK, offsetOK, ratingOK)
	return s.saveLedger(ctx, u.UserID, md.RatingKey, item, next)
}

// moveLedger records both sides' values for the kinds that synced, reporting whether
// anything differs from the old row.
func moveLedger(l ledgerRow, pl plan, m mstate, p Metadata, watchOK, offsetOK, ratingOK bool) (ledgerRow, bool) {
	n := l
	if pl.watchKind && watchOK {
		n.watchSynced, n.mCount, n.pCount = true, m.count, p.ViewCount
	}
	if pl.offsetKind && offsetOK {
		n.offsetSynced, n.mOffset, n.pOffset = true, m.offset, p.ViewOffset
	}
	if pl.ratingKind && ratingOK {
		n.ratingSynced, n.mRating, n.pRating = true, m.rating, plexRating(p.UserRating)
	}
	changed := n.watchSynced != l.watchSynced || n.mCount != l.mCount || n.pCount != l.pCount ||
		n.offsetSynced != l.offsetSynced || n.mOffset != l.mOffset || n.pOffset != l.pOffset ||
		n.ratingSynced != l.ratingSynced || !sameRating(n.mRating, l.mRating) || !sameRating(n.pRating, l.pRating)
	return n, changed
}

// applyMarquee writes pulled values, dating each with Plex's time for it (so the change
// triggers keep that time instead of now).
func (s *Service) applyMarquee(ctx context.Context, uid, item int64, pl plan) error {
	var sets []string
	var args []any
	if pl.setCount != nil {
		sets = append(sets, "play_count = ?", "play_count_changed_at = ?")
		args = append(args, *pl.setCount, pl.countAt.UTC().Format(isoMS))
		if pl.setOffset == nil {
			// Marked watched or unwatched: no resume point, as when it happens in Marquee.
			sets = append(sets, "view_offset_ms = 0")
		}
		if *pl.setCount > 0 {
			// Watched: off the watchlist, as when it happens in Marquee (movies and episodes).
			sets = append(sets, "watchlisted_at = CASE WHEN (SELECT type FROM items WHERE id = user_item_state.item_id) IN ('movie', 'episode') THEN NULL ELSE watchlisted_at END")
		}
	}
	if pl.setLastViewed != nil {
		sets = append(sets, "last_viewed_at = MAX(COALESCE(last_viewed_at, ''), ?)")
		args = append(args, pl.setLastViewed.UTC().Format(isoMS))
	}
	if pl.setOffset != nil {
		sets = append(sets, "view_offset_ms = ?", "view_offset_changed_at = ?")
		args = append(args, *pl.setOffset, pl.offsetAt.UTC().Format(isoMS))
	}
	if pl.setRating {
		sets = append(sets, "rating = ?", "rating_changed_at = ?")
		args = append(args, pl.newRating, pl.ratingAt.UTC().Format(isoMS))
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_item_state(user_id, item_id) VALUES (?, ?)`, uid, item); err != nil {
		return err
	}
	args = append(args, uid, item)
	if _, err := tx.ExecContext(ctx, `UPDATE user_item_state SET `+strings.Join(sets, ", ")+`,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE user_id = ? AND item_id = ?`, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// pullHistory copies plays recorded in Plex since the last run into Marquee's play history
// (as the Plex import does), leaving out the plays this sync pushed to Plex.
func (s *Service) pullHistory(ctx context.Context, srv *client, token string, users []User, keyToItem map[string]int64,
	st *state, rep *Report, dry bool) error {
	accountUser, names := map[int64]int64{}, map[int64]string{}
	for _, u := range users {
		if u.UserID != 0 {
			accountUser[u.PlexAccountID], names[u.PlexAccountID] = u.UserID, u.Name
		}
	}
	if len(accountUser) == 0 {
		return nil
	}
	q := url.Values{"sort": {"viewedAt:asc"}}
	if st.HistorySince > 0 {
		q.Set("viewedAt>", strconv.FormatInt(st.HistorySince, 10)) // viewedAt>=since
	}
	pageSize := s.PageSize
	if pageSize <= 0 {
		pageSize = 500
	}
	newest := st.HistorySince
	err := srv.list(ctx, "/status/sessions/history/all", q, token, pageSize, func(h Metadata) error {
		// The listing starts at the newest play seen last time, which was handled then.
		if h.ViewedAt <= 0 || h.ViewedAt <= st.HistorySince {
			return nil
		}
		if h.ViewedAt > newest {
			newest = h.ViewedAt
		}
		uid, ok := accountUser[h.AccountID]
		if !ok {
			return nil
		}
		if s.pushedPlay(ctx, uid, h.RatingKey, h.ViewedAt, dry) {
			return nil
		}
		title := h.DisplayTitle()
		started := time.Unix(h.ViewedAt, 0).UTC().Format(isoMS)
		var exists int
		s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM play_history WHERE user_id = ? AND started_at = ? AND item_title = ? AND source = 'plex'`,
			uid, started, title).Scan(&exists)
		if exists > 0 {
			return nil
		}
		if !dry {
			var item any
			if id, ok := keyToItem[h.RatingKey]; ok {
				item = id
			}
			if _, err := s.DB.ExecContext(ctx, `INSERT INTO play_history(user_id, item_id, item_title, started_at, stopped_at, source)
				VALUES (?, ?, ?, ?, ?, 'plex')`, uid, item, title, started, started); err != nil {
				return err
			}
		}
		rep.HistoryPulled++
		rep.add(Change{Direction: "toMarquee", User: names[h.AccountID], Title: title, Kind: "history", To: started})
		return nil
	})
	if errors.Is(err, ErrUnreachable) || ctx.Err() != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err != nil {
		rep.fail("play history: " + srv.secrets.redact(err.Error()))
		return nil
	}
	if !dry {
		st.HistorySince = newest
	}
	return nil
}

// pushedPlay reports (and, unless dry, uses up) a play this sync pushed to Plex that Plex
// recorded at viewedAt.
func (s *Service) pushedPlay(ctx context.Context, uid int64, ratingKey string, viewedAt int64, dry bool) bool {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM plex_sync_scrobbles WHERE user_id = ? AND rating_key = ? AND pushed_at BETWEEN ? AND ?
		ORDER BY ABS(pushed_at - ?) LIMIT 1`, uid, ratingKey, viewedAt-300, viewedAt+300, viewedAt).Scan(&id)
	if err != nil {
		return false
	}
	if !dry {
		s.DB.ExecContext(ctx, `DELETE FROM plex_sync_scrobbles WHERE id = ?`, id)
	}
	return true
}
