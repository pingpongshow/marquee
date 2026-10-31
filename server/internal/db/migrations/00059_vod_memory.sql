-- +goose Up
-- What each person watched and rated on demand (LIVE-6), kept by the title itself (its TMDB
-- id, else its title and year) rather than by the source's row, so it outlives a film or
-- series leaving its source and still shapes their suggestions, and comes back with the title
-- if it returns. Filled from the watched marks already kept when the server starts.
CREATE TABLE vod_memory (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('movie', 'series')),
    ident      TEXT NOT NULL,              -- tmdb:{id}, or title:{lower-case title}|{year}
    tmdb_id    INTEGER,
    title      TEXT NOT NULL,
    year       INTEGER,
    genres     TEXT,                       -- JSON array
    keywords   TEXT,                       -- JSON array, lower case
    tmdb_recs  TEXT,                       -- JSON array of TMDB ids, best first
    rating     REAL,                       -- their rating, 0–10 (half stars); NULL = none
    finished   INTEGER NOT NULL DEFAULT 0, -- a film watched to the end
    episodes   INTEGER NOT NULL DEFAULT 0, -- a series' episodes watched
    total      INTEGER NOT NULL DEFAULT 0, -- a series' episodes, when last seen
    watched_at TEXT,
    rated_at   TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, kind, ident)
);
CREATE INDEX vod_memory_tmdb ON vod_memory(tmdb_id) WHERE tmdb_id IS NOT NULL;

-- +goose Down
DROP INDEX vod_memory_tmdb;
DROP TABLE vod_memory;
