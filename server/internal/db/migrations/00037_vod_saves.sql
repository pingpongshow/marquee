-- +goose Up
-- Saving films and episodes from movies-on-demand (Xtream Codes) sources into the library
-- (admins only). The queue survives restarts: an interrupted download resumes from its
-- .part file with a Range request. Links hold the source's password, so only the source's
-- "{id}.{ext}" is kept and the link is built when downloading.
CREATE TABLE vod_saves (
    id             INTEGER PRIMARY KEY,
    source_id      TEXT NOT NULL,
    kind           TEXT NOT NULL,                -- movie, episode
    vod_id         INTEGER NOT NULL,             -- vod_movies.id / vod_episodes.id when queued
    series_id      INTEGER,                      -- vod_series.id (episodes)
    source_key     TEXT NOT NULL,                -- the source's id for the film or episode
    file           TEXT NOT NULL,                -- "{id}.{ext}" on the source
    title          TEXT NOT NULL,                -- shown in the queue
    tmdb_id        INTEGER,
    expected_ms    INTEGER NOT NULL DEFAULT 0,   -- the source's runtime, for verification (0 = unknown)
    target_path    TEXT NOT NULL,
    quality        TEXT NOT NULL,                -- original, remux, transcode (when queued)
    state          TEXT NOT NULL DEFAULT 'queued', -- queued, downloading, processing, verifying, paused, done, failed, cancelled, skipped
    queue_pos      INTEGER NOT NULL,             -- lower runs first
    bytes_done     INTEGER NOT NULL DEFAULT 0,
    bytes_total    INTEGER NOT NULL DEFAULT 0,
    attempts       INTEGER NOT NULL DEFAULT 0,   -- network failures in a row
    verify_tries   INTEGER NOT NULL DEFAULT 0,   -- verification failures (one re-download)
    next_attempt_at TEXT,                        -- a retry waits until then
    retryable      INTEGER NOT NULL DEFAULT 1,   -- failed items the background retry may queue again
    error          TEXT NOT NULL DEFAULT '',
    verified       INTEGER NOT NULL DEFAULT 0,
    verify_note    TEXT NOT NULL DEFAULT '',
    sha256         TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    started_at     TEXT,
    finished_at    TEXT,
    requested_by   INTEGER REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX vod_saves_queue ON vod_saves(state, source_id, queue_pos);
CREATE INDEX vod_saves_item ON vod_saves(source_id, kind, source_key);

-- +goose Down
DROP TABLE vod_saves;
