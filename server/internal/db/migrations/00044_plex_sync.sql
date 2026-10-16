-- +goose Up
-- Two-way Plex sync: per-field change times on watch state, the Plex account ↔ Marquee user
-- mapping, and the sync ledger.

-- When each synced value last changed in Marquee. Triggers keep them current for every
-- writer; a writer that sets one explicitly (the Plex sync, carrying Plex's own time) keeps
-- its value. Rows from before this migration have NULL and fall back to last_viewed_at /
-- updated_at.
ALTER TABLE user_item_state ADD COLUMN play_count_changed_at TEXT;
ALTER TABLE user_item_state ADD COLUMN view_offset_changed_at TEXT;
ALTER TABLE user_item_state ADD COLUMN rating_changed_at TEXT;

-- +goose StatementBegin
CREATE TRIGGER user_item_state_changed_insert AFTER INSERT ON user_item_state
WHEN (NEW.play_count > 0 AND NEW.play_count_changed_at IS NULL)
  OR (NEW.view_offset_ms > 0 AND NEW.view_offset_changed_at IS NULL)
  OR (NEW.rating IS NOT NULL AND NEW.rating_changed_at IS NULL)
BEGIN
  UPDATE user_item_state SET
    play_count_changed_at = COALESCE(play_count_changed_at, CASE WHEN play_count > 0 THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') END),
    view_offset_changed_at = COALESCE(view_offset_changed_at, CASE WHEN view_offset_ms > 0 THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') END),
    rating_changed_at = COALESCE(rating_changed_at, CASE WHEN rating IS NOT NULL THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') END)
  WHERE user_id = NEW.user_id AND item_id = NEW.item_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER user_item_state_play_count_changed AFTER UPDATE OF play_count ON user_item_state
WHEN OLD.play_count IS NOT NEW.play_count AND NEW.play_count_changed_at IS OLD.play_count_changed_at
BEGIN
  UPDATE user_item_state SET play_count_changed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE user_id = NEW.user_id AND item_id = NEW.item_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER user_item_state_view_offset_changed AFTER UPDATE OF view_offset_ms ON user_item_state
WHEN OLD.view_offset_ms IS NOT NEW.view_offset_ms AND NEW.view_offset_changed_at IS OLD.view_offset_changed_at
BEGIN
  UPDATE user_item_state SET view_offset_changed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE user_id = NEW.user_id AND item_id = NEW.item_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER user_item_state_rating_changed AFTER UPDATE OF rating ON user_item_state
WHEN OLD.rating IS NOT NEW.rating AND NEW.rating_changed_at IS OLD.rating_changed_at
BEGIN
  UPDATE user_item_state SET rating_changed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE user_id = NEW.user_id AND item_id = NEW.item_id;
END;
-- +goose StatementEnd

-- Plex accounts (Plex's own account ids: 1 is the server owner) and the Marquee user each
-- syncs with. Filled by the Plex import and by each sync; the admin edits user_id.
CREATE TABLE plex_sync_users (
    plex_account_id INTEGER PRIMARY KEY,
    name            TEXT NOT NULL DEFAULT '',
    user_id         INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind            TEXT NOT NULL DEFAULT 'unknown', -- owner, home, managed, shared, unknown
    can_sync        INTEGER NOT NULL DEFAULT 0,
    reason          TEXT NOT NULL DEFAULT '',        -- why it can't sync
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- The sync ledger: per (Marquee user, Plex item) the values each side had after the last
-- sync. A side whose value differs from its ledger value changed since; equal values on
-- both sides are skipped without any write. *_synced is 0 until that kind first synced.
CREATE TABLE plex_sync_ledger (
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating_key     TEXT NOT NULL,
    item_id        INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    watch_synced   INTEGER NOT NULL DEFAULT 0,
    m_play_count   INTEGER NOT NULL DEFAULT 0,
    p_view_count   INTEGER NOT NULL DEFAULT 0,
    offset_synced  INTEGER NOT NULL DEFAULT 0,
    m_offset_ms    INTEGER NOT NULL DEFAULT 0,
    p_offset_ms    INTEGER NOT NULL DEFAULT 0,
    rating_synced  INTEGER NOT NULL DEFAULT 0,
    m_rating       REAL,
    p_rating       REAL,
    synced_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (user_id, rating_key)
);
CREATE INDEX plex_sync_ledger_item ON plex_sync_ledger(item_id);

-- Plays pushed to Plex (scrobbles). Plex records them in its history at push time; the
-- history pull skips them so they don't come back as new plays.
CREATE TABLE plex_sync_scrobbles (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating_key TEXT NOT NULL,
    pushed_at  INTEGER NOT NULL -- unix seconds
);
CREATE INDEX plex_sync_scrobbles_key ON plex_sync_scrobbles(user_id, rating_key);

-- +goose Down
DROP TABLE plex_sync_scrobbles;
DROP TABLE plex_sync_ledger;
DROP TABLE plex_sync_users;
DROP TRIGGER user_item_state_rating_changed;
DROP TRIGGER user_item_state_view_offset_changed;
DROP TRIGGER user_item_state_play_count_changed;
DROP TRIGGER user_item_state_changed_insert;
ALTER TABLE user_item_state DROP COLUMN rating_changed_at;
ALTER TABLE user_item_state DROP COLUMN view_offset_changed_at;
ALTER TABLE user_item_state DROP COLUMN play_count_changed_at;
