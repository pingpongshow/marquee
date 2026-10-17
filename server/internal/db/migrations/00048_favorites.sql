-- +goose Up
-- Favourites (the heart): when the person marked a movie, show, episode, song, album or
-- artist as a favourite, NULL when it isn't one. Per user, so managed profiles have their own.
ALTER TABLE user_item_state ADD COLUMN favorite_at TEXT;
CREATE INDEX user_item_state_favorite ON user_item_state(user_id, favorite_at DESC) WHERE favorite_at IS NOT NULL;

-- +goose Down
DROP INDEX user_item_state_favorite;
ALTER TABLE user_item_state DROP COLUMN favorite_at;
