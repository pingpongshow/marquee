-- +goose Up
-- DVR (LIVE-5): series rules and recordings of guide programmes.
CREATE TABLE dvr_rules (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,             -- matched against guide titles, ignoring case
    channel_id  INTEGER REFERENCES live_channels(id) ON DELETE CASCADE, -- NULL = any channel
    created_at  TEXT NOT NULL
);

CREATE TABLE dvr_recordings (
    id           INTEGER PRIMARY KEY,
    rule_id      INTEGER REFERENCES dvr_rules(id) ON DELETE SET NULL,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id   INTEGER REFERENCES live_channels(id) ON DELETE SET NULL,
    channel_name TEXT NOT NULL,
    start        TEXT NOT NULL,            -- the programme's times (UTC, RFC 3339)
    stop         TEXT NOT NULL,
    title        TEXT NOT NULL,
    subtitle     TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    category     TEXT NOT NULL DEFAULT '',
    episode      TEXT NOT NULL DEFAULT '',
    image_url    TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'scheduled', -- scheduled, recording, completed, failed, cancelled
    path         TEXT NOT NULL DEFAULT '',
    size         INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    UNIQUE (channel_id, start)
);
CREATE INDEX dvr_recordings_status ON dvr_recordings(status, start);

-- +goose Down
DROP TABLE dvr_recordings;
DROP TABLE dvr_rules;
