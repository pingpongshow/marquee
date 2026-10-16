-- +goose Up
-- Comparing films and episodes on demand with the library's copies (LIVE-6), upgrading a
-- library copy from a better one, and films and episodes on demand in Continue Watching.

-- The quality of a film's or episode's file on its source: from the source's details
-- (Xtream Codes "info.video") or, when it gives none, from probing the file (admins
-- opening it; one at a time). Refreshed after a while.
CREATE TABLE vod_quality (
    id           INTEGER PRIMARY KEY,
    movie_id     INTEGER REFERENCES vod_movies(id) ON DELETE CASCADE,
    episode_id   INTEGER REFERENCES vod_episodes(id) ON DELETE CASCADE,
    width        INTEGER NOT NULL DEFAULT 0,
    height       INTEGER NOT NULL DEFAULT 0,
    video_codec  TEXT NOT NULL DEFAULT '',
    hdr          TEXT NOT NULL DEFAULT '',   -- '' = SDR; HDR10, HLG, DV…
    bitrate_kbps INTEGER NOT NULL DEFAULT 0, -- the whole file's (0 = unknown)
    origin       TEXT NOT NULL,              -- info (the source's details), probe
    error        TEXT NOT NULL DEFAULT '',   -- the probe failed (tried again later)
    checked_at   TEXT NOT NULL,
    CHECK ((movie_id IS NULL) <> (episode_id IS NULL))
);
CREATE UNIQUE INDEX vod_quality_movie ON vod_quality(movie_id) WHERE movie_id IS NOT NULL;
CREATE UNIQUE INDEX vod_quality_episode ON vod_quality(episode_id) WHERE episode_id IS NOT NULL;

-- An upgrade replaces the library's copy: the file it replaces (moved to the media trash
-- once the new one is verified) and where that file was.
ALTER TABLE vod_saves ADD COLUMN replace_file_id INTEGER;
ALTER TABLE vod_saves ADD COLUMN replaced_path TEXT NOT NULL DEFAULT '';

-- Films and series on demand removed from Continue Watching, until watched again after.
CREATE TABLE vod_continue_hidden (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    movie_id  INTEGER REFERENCES vod_movies(id) ON DELETE CASCADE,
    series_id INTEGER REFERENCES vod_series(id) ON DELETE CASCADE,
    hidden_at TEXT NOT NULL,
    CHECK ((movie_id IS NULL) <> (series_id IS NULL))
);
CREATE UNIQUE INDEX vod_continue_hidden_movie ON vod_continue_hidden(user_id, movie_id) WHERE movie_id IS NOT NULL;
CREATE UNIQUE INDEX vod_continue_hidden_series ON vod_continue_hidden(user_id, series_id) WHERE series_id IS NOT NULL;

-- Continue Watching reads in-progress films and episodes newest first.
CREATE INDEX vod_progress_recent ON vod_progress(user_id, updated_at);
CREATE INDEX vod_episode_progress_recent ON vod_episode_progress(user_id, updated_at);

-- +goose Down
DROP INDEX vod_episode_progress_recent;
DROP INDEX vod_progress_recent;
DROP TABLE vod_continue_hidden;
ALTER TABLE vod_saves DROP COLUMN replaced_path;
ALTER TABLE vod_saves DROP COLUMN replace_file_id;
DROP TABLE vod_quality;
