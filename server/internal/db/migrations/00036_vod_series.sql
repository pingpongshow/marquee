-- +goose Up
-- TV series on demand (LIVE-6) from Xtream Codes sources. The list comes with each refresh;
-- a series' seasons and episodes are fetched when it is first opened, and again once stale
-- (episodes_at). Episode links hold the source's password, so only "{id}.{ext}" is kept.
CREATE TABLE vod_series (
    id             INTEGER PRIMARY KEY,
    source_id      TEXT NOT NULL,
    source_key     TEXT NOT NULL,            -- xtream:{series_id}
    title          TEXT NOT NULL,
    category       TEXT NOT NULL DEFAULT '',
    logo_url       TEXT NOT NULL DEFAULT '', -- the source's cover
    year           INTEGER,
    overview       TEXT NOT NULL DEFAULT '',
    genres         TEXT NOT NULL DEFAULT '[]',
    content_rating TEXT NOT NULL DEFAULT '',
    rating         REAL,
    director       TEXT NOT NULL DEFAULT '',
    cast_json      TEXT NOT NULL DEFAULT '[]',
    poster_url     TEXT NOT NULL DEFAULT '',
    backdrop_url   TEXT NOT NULL DEFAULT '',
    tmdb_id        INTEGER,
    match_state    TEXT NOT NULL DEFAULT 'pending', -- pending (TMDB id to look up), matched, failed, none (no id)
    last_modified  TEXT NOT NULL DEFAULT '',     -- the source's, to notice changed episodes
    episodes_at    TEXT,                         -- seasons and episodes fetched
    added_at       TEXT NOT NULL,
    UNIQUE (source_id, source_key)
);
CREATE INDEX vod_series_category ON vod_series(category, title);

CREATE TABLE vod_seasons (
    series_id  INTEGER NOT NULL REFERENCES vod_series(id) ON DELETE CASCADE,
    season     INTEGER NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    overview   TEXT NOT NULL DEFAULT '',
    poster_url TEXT NOT NULL DEFAULT '',
    air_date   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (series_id, season)
);

CREATE TABLE vod_episodes (
    id         INTEGER PRIMARY KEY,
    series_id  INTEGER NOT NULL REFERENCES vod_series(id) ON DELETE CASCADE,
    source_key TEXT NOT NULL,                -- the source's episode id
    season     INTEGER NOT NULL,
    episode    INTEGER NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    overview   TEXT NOT NULL DEFAULT '',
    still_url  TEXT NOT NULL DEFAULT '',
    air_date   TEXT NOT NULL DEFAULT '',
    runtime_ms INTEGER NOT NULL DEFAULT 0,
    rating     REAL,
    stream_url TEXT NOT NULL,                -- "{id}.{ext}"; the link is built when playing
    tmdb       INTEGER NOT NULL DEFAULT 0,   -- title, overview and still are TMDB's (kept on refetch)
    UNIQUE (series_id, source_key)
);
CREATE INDEX vod_episodes_order ON vod_episodes(series_id, season, episode);

CREATE TABLE vod_episode_progress (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    episode_id  INTEGER NOT NULL REFERENCES vod_episodes(id) ON DELETE CASCADE,
    series_id   INTEGER NOT NULL REFERENCES vod_series(id) ON DELETE CASCADE,
    position_ms INTEGER NOT NULL DEFAULT 0,
    watched     INTEGER NOT NULL DEFAULT 0,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (user_id, episode_id)
);
CREATE INDEX vod_episode_progress_series ON vod_episode_progress(user_id, series_id, updated_at);

ALTER TABLE vod_sources ADD COLUMN series INTEGER NOT NULL DEFAULT 0;

-- Filters (year, rating; source is the start of the unique index) on large catalogues.
CREATE INDEX vod_series_year ON vod_series(year);
CREATE INDEX vod_series_rating ON vod_series(rating);
CREATE INDEX vod_movies_year ON vod_movies(year);
CREATE INDEX vod_movies_rating ON vod_movies(rating);

-- +goose Down
DROP INDEX vod_movies_rating;
DROP INDEX vod_movies_year;
ALTER TABLE vod_sources DROP COLUMN series;
DROP TABLE vod_episode_progress;
DROP TABLE vod_episodes;
DROP TABLE vod_seasons;
DROP TABLE vod_series;
