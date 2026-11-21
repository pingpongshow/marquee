-- +goose Up
-- A thumbs up (USER-21): someone liked a song, film or show. Not a favourite: it steers
-- suggestions, radios and mixes towards things like it. A thumbs down (disliked_at) now covers
-- films and shows too.
ALTER TABLE user_item_state ADD COLUMN liked_at TEXT;
CREATE INDEX user_item_state_liked ON user_item_state(user_id) WHERE liked_at IS NOT NULL;

-- +goose Down
DROP INDEX user_item_state_liked;
ALTER TABLE user_item_state DROP COLUMN liked_at;
