-- +goose Up
-- Live TV per user (LIVE-4): hidden channels and recently watched.
CREATE TABLE live_hidden (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES live_channels(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, channel_id)
);
CREATE TABLE live_recent (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES live_channels(id) ON DELETE CASCADE,
    watched_at TEXT NOT NULL,
    PRIMARY KEY (user_id, channel_id)
);
CREATE INDEX live_recent_user ON live_recent(user_id, watched_at DESC);

-- +goose Down
DROP TABLE live_recent;
DROP TABLE live_hidden;
