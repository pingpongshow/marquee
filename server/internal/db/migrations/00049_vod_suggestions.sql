-- +goose Up
-- Suggestions from the films and series on demand (Watch on VOD, Also on VOD, Muse).
--
-- TMDB's recommendations ("people who liked this also liked", first page, TMDB ids in rank
-- order, a JSON array), plot keywords (TMDB's lower-case names, a JSON array) and vote count
-- of a matched film or series, kept from the match; NULL until known (titles matched before
-- these were kept get them from the "Fetch VOD suggestion data" task).
ALTER TABLE vod_movies ADD COLUMN tmdb_recs TEXT;
ALTER TABLE vod_movies ADD COLUMN keywords TEXT;
ALTER TABLE vod_movies ADD COLUMN votes INTEGER;
ALTER TABLE vod_series ADD COLUMN tmdb_recs TEXT;
ALTER TABLE vod_series ADD COLUMN keywords TEXT;
ALTER TABLE vod_series ADD COLUMN votes INTEGER;
CREATE INDEX vod_movies_tmdb ON vod_movies(tmdb_id) WHERE match_state = 'matched';
CREATE INDEX vod_series_tmdb ON vod_series(tmdb_id) WHERE match_state = 'matched';

-- "Not interested": films and series a person asked not to be suggested.
CREATE TABLE vod_suggestion_hidden (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind      TEXT NOT NULL CHECK (kind IN ('movie', 'series')),
    vod_id    INTEGER NOT NULL,
    hidden_at TEXT NOT NULL,
    PRIMARY KEY (user_id, kind, vod_id)
);

-- +goose Down
DROP TABLE vod_suggestion_hidden;
DROP INDEX vod_series_tmdb;
DROP INDEX vod_movies_tmdb;
ALTER TABLE vod_series DROP COLUMN votes;
ALTER TABLE vod_series DROP COLUMN keywords;
ALTER TABLE vod_series DROP COLUMN tmdb_recs;
ALTER TABLE vod_movies DROP COLUMN votes;
ALTER TABLE vod_movies DROP COLUMN keywords;
ALTER TABLE vod_movies DROP COLUMN tmdb_recs;
