package items

import (
	"context"
	"time"
)

// Changes made offline (USER-18): apps queue ratings, watched marks, watchlist changes and
// comments made without a connection and send them, with the time they were made, when they
// reconnect. Fresh records the change's time and reports whether it is newer than the last
// change of that kind to the item; an older one is skipped so it can't undo a newer change
// made elsewhere. A nil time means now.
func (s *Store) Fresh(ctx context.Context, uid, itemID int64, field string, at *time.Time) (bool, error) {
	when := time.Now().UTC()
	if at != nil && !at.IsZero() {
		when = at.UTC()
		if when.After(time.Now().Add(time.Minute)) {
			when = time.Now().UTC() // a device clock ahead of ours
		}
	}
	stamp := when.Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `INSERT INTO change_clock (user_id, item_id, field, at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, item_id, field) DO UPDATE SET at = excluded.at WHERE excluded.at >= change_clock.at`,
		uid, itemID, field, stamp)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
