-- +goose Up
-- A title leaves someone's watchlist once they've watched it (USER-8): a film or video when it
-- becomes watched, a show when its last unwatched episode does. Only the step from unwatched to
-- watched counts, so a title put back on the watchlist afterwards stays there.
-- +goose StatementBegin
CREATE TRIGGER watchlist_done_update AFTER UPDATE OF play_count ON user_item_state
WHEN NEW.play_count > 0 AND OLD.play_count = 0
BEGIN
    UPDATE user_item_state SET watchlisted_at = NULL
    WHERE user_id = NEW.user_id AND item_id = NEW.item_id AND watchlisted_at IS NOT NULL
      AND (SELECT type FROM items WHERE id = NEW.item_id) IN ('movie', 'video');
    UPDATE user_item_state SET watchlisted_at = NULL
    WHERE user_id = NEW.user_id AND watchlisted_at IS NOT NULL
      AND item_id = (SELECT grandparent_id FROM items WHERE id = NEW.item_id AND type = 'episode')
      AND NOT EXISTS (SELECT 1 FROM items e WHERE e.grandparent_id = user_item_state.item_id AND e.type = 'episode'
          AND e.extra_type IS NULL
          AND NOT EXISTS (SELECT 1 FROM user_item_state x WHERE x.user_id = NEW.user_id AND x.item_id = e.id AND x.play_count > 0));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER watchlist_done_insert AFTER INSERT ON user_item_state
WHEN NEW.play_count > 0
BEGIN
    UPDATE user_item_state SET watchlisted_at = NULL
    WHERE user_id = NEW.user_id AND watchlisted_at IS NOT NULL
      AND item_id = (SELECT grandparent_id FROM items WHERE id = NEW.item_id AND type = 'episode')
      AND NOT EXISTS (SELECT 1 FROM items e WHERE e.grandparent_id = user_item_state.item_id AND e.type = 'episode'
          AND e.extra_type IS NULL
          AND NOT EXISTS (SELECT 1 FROM user_item_state x WHERE x.user_id = NEW.user_id AND x.item_id = e.id AND x.play_count > 0));
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER watchlist_done_insert;
DROP TRIGGER watchlist_done_update;
