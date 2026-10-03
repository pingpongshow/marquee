package items

import (
	"context"
	"strings"
	"time"
)

// Reviews (USER-17): each person's rating stays theirs; the community rating is everyone's
// average, shown with the comments people leave.

type Review struct {
	UserID    int64
	UserName  string
	Avatar    int64
	Rating    float64 // 0 = not rated
	Comment   string
	UpdatedAt time.Time
}

// Reviews lists everyone who rated or commented on an item: comments first, newest first.
func (s *Store) Reviews(ctx context.Context, itemID int64) (avg float64, count int, list []Review, err error) {
	s.db.QueryRowContext(ctx, `SELECT COALESCE(AVG(rating), 0), COUNT(*) FROM user_item_state WHERE item_id = ? AND rating > 0`, itemID).Scan(&avg, &count)
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.display_name, COALESCE(u.avatar_version, 0), COALESCE(st.rating, 0), COALESCE(r.comment, ''),
			COALESCE(r.updated_at, st.updated_at, '')
		FROM users u
		LEFT JOIN user_item_state st ON st.user_id = u.id AND st.item_id = ?
		LEFT JOIN item_reviews r ON r.user_id = u.id AND r.item_id = ?
		WHERE (st.rating > 0 OR r.comment IS NOT NULL)
		ORDER BY r.comment IS NULL, COALESCE(r.updated_at, st.updated_at) DESC`, itemID, itemID)
	if err != nil {
		return 0, 0, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Review
		var at string
		if err := rows.Scan(&r.UserID, &r.UserName, &r.Avatar, &r.Rating, &r.Comment, &at); err != nil {
			return 0, 0, nil, err
		}
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, at)
		list = append(list, r)
	}
	return avg, count, list, rows.Err()
}

// SetComment saves (or with an empty comment removes) a person's comment on an item.
func (s *Store) SetComment(ctx context.Context, userID, itemID int64, comment string) error {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return s.DeleteComment(ctx, userID, itemID)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO item_reviews (user_id, item_id, comment, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET comment = excluded.comment, updated_at = excluded.updated_at`,
		userID, itemID, comment, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) DeleteComment(ctx context.Context, userID, itemID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM item_reviews WHERE user_id = ? AND item_id = ?`, userID, itemID)
	return err
}
