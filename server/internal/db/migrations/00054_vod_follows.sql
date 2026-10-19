-- +goose Up
-- Following series on demand: new episodes are saved to the library automatically. A
-- follow is keyed by the source and the source's series id (vod_series.source_key), so it
-- survives catalogue refreshes the way saves do.
CREATE TABLE vod_follows (
    id               INTEGER PRIMARY KEY,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_id        TEXT NOT NULL,
    series_key       TEXT NOT NULL,                 -- vod_series.source_key
    series_id        INTEGER NOT NULL DEFAULT 0,    -- vod_series.id when last seen (ids change if a series leaves and comes back)
    title            TEXT NOT NULL DEFAULT '',      -- the series' title when last seen
    max_bytes        INTEGER NOT NULL DEFAULT 0,    -- its own size limit (0 = none)
    include_existing INTEGER NOT NULL DEFAULT 0,    -- also save the episodes there when followed
    baselined        INTEGER NOT NULL DEFAULT 0,    -- the episodes there when followed are known
    destination      TEXT NOT NULL DEFAULT '',      -- a save folder's id ('' = the shows folder)
    status           TEXT NOT NULL DEFAULT 'following', -- following, full, quota, paused
    status_text      TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    last_new_at      TEXT,                          -- when it last queued an episode
    UNIQUE (user_id, source_id, series_key)
);

-- The episodes a follower's follows have dealt with, by the source's episode id: those there
-- when followed and those passed over (save_id NULL, path ''), and those queued (save_id)
-- and saved (path, bytes: the file's size when saved). Saved files count towards the
-- follower's DVR quota while they're in the library and the series is followed.
CREATE TABLE vod_follow_episodes (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_id   TEXT NOT NULL,
    series_key  TEXT NOT NULL,
    episode_key TEXT NOT NULL,
    save_id     INTEGER,
    path        TEXT NOT NULL DEFAULT '',
    bytes       INTEGER NOT NULL DEFAULT 0,
    saved_at    TEXT,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (user_id, source_id, episode_key)
);
CREATE INDEX vod_follow_episodes_series ON vod_follow_episodes(user_id, source_id, series_key);
CREATE INDEX vod_follow_episodes_save ON vod_follow_episodes(save_id) WHERE save_id IS NOT NULL;

-- +goose Down
DROP TABLE vod_follow_episodes;
DROP TABLE vod_follows;
