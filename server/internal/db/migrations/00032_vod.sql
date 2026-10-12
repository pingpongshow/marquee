-- +goose Up
-- Movies on demand (LIVE-6): films from M3U sources such as Pluto TV, shown under Live TV.
-- Nothing is downloaded; stream_url is the source's (expiring) link, refreshed with the list.
CREATE TABLE vod_movies (
    id             INTEGER PRIMARY KEY,
    source_id      TEXT NOT NULL,
    source_key     TEXT NOT NULL,            -- the source's stable id for the film (not its URL)
    title          TEXT NOT NULL,
    category       TEXT NOT NULL DEFAULT '',
    stream_url     TEXT NOT NULL,
    logo_url       TEXT NOT NULL DEFAULT '',
    year           INTEGER,
    overview       TEXT NOT NULL DEFAULT '',
    tagline        TEXT NOT NULL DEFAULT '',
    genres         TEXT NOT NULL DEFAULT '[]',  -- JSON array
    content_rating TEXT NOT NULL DEFAULT '',
    rating         REAL,                         -- TMDB vote average, 0-10
    runtime_ms     INTEGER NOT NULL DEFAULT 0,
    director       TEXT NOT NULL DEFAULT '',
    cast_json      TEXT NOT NULL DEFAULT '[]',  -- JSON array of {name, character}
    poster_url     TEXT NOT NULL DEFAULT '',
    backdrop_url   TEXT NOT NULL DEFAULT '',
    tmdb_id        INTEGER,
    imdb_id        TEXT NOT NULL DEFAULT '',
    match_state    TEXT NOT NULL DEFAULT 'pending', -- pending, matched, failed
    details_at     TEXT,                         -- source catalogue details fetched
    added_at       TEXT NOT NULL,
    UNIQUE (source_id, source_key)
);
CREATE INDEX vod_movies_category ON vod_movies(category, title);

CREATE TABLE vod_sources (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    movies       INTEGER NOT NULL DEFAULT 0,
    refreshed_at TEXT,
    error        TEXT
);

CREATE TABLE vod_progress (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    movie_id    INTEGER NOT NULL REFERENCES vod_movies(id) ON DELETE CASCADE,
    position_ms INTEGER NOT NULL DEFAULT 0,
    watched     INTEGER NOT NULL DEFAULT 0,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (user_id, movie_id)
);

-- +goose Down
DROP TABLE vod_progress;
DROP TABLE vod_sources;
DROP TABLE vod_movies;
