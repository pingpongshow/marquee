-- +goose Up
-- Smart collections (META-7): a collection whose members are a saved library filter.
CREATE TABLE smart_collections (
    item_id   INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    item_type TEXT NOT NULL,            -- movie or show: what it lists
    filter    TEXT NOT NULL DEFAULT '{}', -- items.Filter as JSON
    sort      TEXT NOT NULL DEFAULT 'title',
    max_items INTEGER NOT NULL DEFAULT 0  -- 0 = all
);

-- Each person's Home: row order, hidden rows and pinned collections/playlists.
CREATE TABLE user_home_layout (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    layout  TEXT NOT NULL DEFAULT '{}'
);

-- Subtitle and audio timing a person set for a file, remembered for next time.
CREATE TABLE playback_offsets (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_id     INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    subtitle_ms INTEGER NOT NULL DEFAULT 0,
    audio_ms    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, file_id)
);

-- Sharing: links that let a friend outside the household make an account.
CREATE TABLE invites (
    id          INTEGER PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    note        TEXT NOT NULL DEFAULT '',
    restrictions TEXT NOT NULL DEFAULT '{}', -- what the new account gets (libraries, ratings, remote)
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    used_at     TEXT
);

-- +goose Down
DROP TABLE invites;
DROP TABLE playback_offsets;
DROP TABLE user_home_layout;
DROP TABLE smart_collections;
