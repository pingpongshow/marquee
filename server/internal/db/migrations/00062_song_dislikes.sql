-- +goose Up
-- Each device has its own conversion: the first to finish deletes it, which mustn't leave
-- another device with nothing to fetch.
ALTER TABLE download_jobs ADD COLUMN device_id INTEGER;
-- A song someone gave a thumbs down (MUSIC-25): radios and generated playlists leave it out.
ALTER TABLE user_item_state ADD COLUMN disliked_at TEXT;
CREATE INDEX user_item_state_disliked ON user_item_state(user_id) WHERE disliked_at IS NOT NULL;

-- +goose Down
DROP INDEX user_item_state_disliked;
ALTER TABLE user_item_state DROP COLUMN disliked_at;
ALTER TABLE download_jobs DROP COLUMN device_id;
