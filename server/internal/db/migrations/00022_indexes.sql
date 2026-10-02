-- +goose Up
-- Indexes for foreign keys and frequent lookups (bug sweep, 2026-10-02): deleting items or
-- devices scanned these tables in full, and the DVR matched guide titles without an index.
CREATE INDEX IF NOT EXISTS user_item_state_item ON user_item_state(item_id);
CREATE INDEX IF NOT EXISTS play_history_item ON play_history(item_id);
CREATE INDEX IF NOT EXISTS play_history_device ON play_history(device_id);
CREATE INDEX IF NOT EXISTS playlist_items_item ON playlist_items(item_id);
CREATE INDEX IF NOT EXISTS collection_items_item ON collection_items(item_id);
CREATE INDEX IF NOT EXISTS live_programmes_title ON live_programmes(lower(title));
CREATE INDEX IF NOT EXISTS dvr_recordings_rule ON dvr_recordings(rule_id);

-- +goose Down
DROP INDEX IF EXISTS user_item_state_item;
DROP INDEX IF EXISTS play_history_item;
DROP INDEX IF EXISTS play_history_device;
DROP INDEX IF EXISTS playlist_items_item;
DROP INDEX IF EXISTS collection_items_item;
DROP INDEX IF EXISTS live_programmes_title;
DROP INDEX IF EXISTS dvr_recordings_rule;
