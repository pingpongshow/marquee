-- +goose Up
-- External ratings from OMDb (IMDb, Rotten Tomatoes, Metacritic).
ALTER TABLE items ADD COLUMN imdb_rating REAL;        -- 0–10
ALTER TABLE items ADD COLUMN imdb_votes INTEGER;
ALTER TABLE items ADD COLUMN rt_critic INTEGER;       -- Rotten Tomatoes Tomatometer, 0–100
ALTER TABLE items ADD COLUMN metacritic INTEGER;      -- 0–100
ALTER TABLE items ADD COLUMN ratings_refreshed_at TEXT;

-- +goose Down
SELECT 'irreversible';
