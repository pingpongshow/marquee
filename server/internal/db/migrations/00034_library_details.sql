-- +goose Up
-- More library metadata for Muse and recommendations.
--
-- Movies and shows (from TMDB): the original language (ISO 639-1), production countries as
-- tags of kind 'country' (ISO 3166-1 codes, e.g. 'KR'), keywords as tags of kind 'keyword'
-- (TMDB's lower-case names, e.g. 'time loop'), and TMDB's recommendations ("people who
-- liked this also liked"), first page, by rank. tmdb_details records when the extras were
-- fetched, so the backfill task only fetches each matched item once.
ALTER TABLE items ADD COLUMN original_language TEXT;

CREATE TABLE tmdb_recommendations (
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    tmdb_id INTEGER NOT NULL, -- a movie for a movie, a show for a show
    rank    INTEGER NOT NULL, -- 1 = TMDB's first
    PRIMARY KEY (item_id, tmdb_id)
);

CREATE TABLE tmdb_details (
    item_id    INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    fetched_at TEXT NOT NULL
);

-- Music artists and albums (from MusicBrainz). One row per looked-up item, also when nothing
-- was found (found = 0, retried after 180 days). version lets a newer enrichment revisit items.
-- Artists: type (person, group, orchestra, choir, character, other), gender, country (ISO
-- 3166-1), area and begin area names, active years. Albums: MusicBrainz primary type and label.
-- MusicBrainz tags and genres (top 8 by votes) are tags of kind 'style'.
CREATE TABLE music_info (
    item_id      INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,          -- artist, album
    found        INTEGER NOT NULL DEFAULT 0,
    version      INTEGER NOT NULL DEFAULT 0,
    checked_at   TEXT NOT NULL,
    artist_type  TEXT,
    gender       TEXT,
    country      TEXT,
    area         TEXT,
    begin_area   TEXT,
    begin_year   INTEGER,
    end_year     INTEGER,
    primary_type TEXT,
    label        TEXT
);
CREATE INDEX music_info_country ON music_info(country);

-- Text embeddings of artist and album descriptions (optional AI), like item_embeddings for
-- movies: so music Muse can match "Icelandic female singer-songwriters" to artists.
CREATE TABLE music_embeddings (
    item_id     INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    model       TEXT NOT NULL,
    vector      BLOB NOT NULL,
    text_hash   TEXT NOT NULL,
    embedded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE music_embeddings;
DROP TABLE music_info;
DROP TABLE tmdb_details;
DROP TABLE tmdb_recommendations;
ALTER TABLE items DROP COLUMN original_language;
