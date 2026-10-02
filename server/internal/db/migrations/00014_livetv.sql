-- +goose Up
-- Live TV (LIVE-1..4): channels and guide from M3U/XMLTV sources, and favourites.
CREATE TABLE live_channels (
    id          INTEGER PRIMARY KEY,
    source_id   TEXT NOT NULL,
    source_key  TEXT NOT NULL,           -- tvg-id, else the stream URL
    epg_id      TEXT NOT NULL DEFAULT '', -- the guide's channel id (tvg-id)
    number      TEXT NOT NULL DEFAULT '',
    sort_key    REAL NOT NULL DEFAULT 0,
    name        TEXT NOT NULL,
    group_name  TEXT NOT NULL DEFAULT '',
    logo_url    TEXT NOT NULL DEFAULT '',
    stream_url  TEXT NOT NULL,
    present     INTEGER NOT NULL DEFAULT 1, -- 0 once it disappears from its source
    UNIQUE (source_id, source_key)
);
CREATE INDEX live_channels_sort ON live_channels(present, sort_key, name);

CREATE TABLE live_programmes (
    id          INTEGER PRIMARY KEY,
    channel_id  INTEGER NOT NULL REFERENCES live_channels(id) ON DELETE CASCADE,
    start       TEXT NOT NULL,
    stop        TEXT NOT NULL,
    title       TEXT NOT NULL,
    subtitle    TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT '',
    episode     TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX live_programmes_time ON live_programmes(channel_id, start);
CREATE INDEX live_programmes_stop ON live_programmes(stop);

CREATE TABLE live_favorites (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES live_channels(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, channel_id)
);

CREATE TABLE live_sources (
    id           TEXT PRIMARY KEY,
    refreshed_at TEXT,
    channels     INTEGER NOT NULL DEFAULT 0,
    programmes   INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE live_favorites;
DROP TABLE live_programmes;
DROP TABLE live_channels;
DROP TABLE live_sources;
