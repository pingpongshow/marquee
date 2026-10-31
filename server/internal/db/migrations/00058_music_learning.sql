-- +goose Up
-- Similar artists and community tags from ListenBrainz and Last.fm, for radios, mixes, moods
-- and Muse (MUSIC-15).
CREATE TABLE music_similar_artists (
    artist_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    similar_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    source     TEXT NOT NULL,          -- listenbrainz, lastfm
    score      REAL NOT NULL,          -- 0–1, 1 the most similar of the artist's list
    PRIMARY KEY (artist_id, similar_id, source)
);
CREATE TABLE music_tags (
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE, -- an artist or a track
    tag     TEXT NOT NULL,             -- lower case
    source  TEXT NOT NULL,             -- listenbrainz, lastfm
    weight  REAL NOT NULL,             -- 0–1, 1 the item's strongest tag
    PRIMARY KEY (item_id, tag, source)
);
CREATE INDEX music_tags_tag ON music_tags(tag);
-- When each artist or track was last looked up, so the task continues where it stopped.
CREATE TABLE music_community_checked (
    item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    source     TEXT NOT NULL,          -- listenbrainz-similar, listenbrainz-tags, lastfm-artist, lastfm-track
    checked_at TEXT NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, source)
);

-- Tracks played together in one sitting or kept in one playlist (MUSIC-17); rebuilt nightly.
CREATE TABLE music_coplays (
    a      INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    b      INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    weight REAL NOT NULL,              -- 0–1, 1 the track's strongest pairing
    PRIMARY KEY (a, b)
);

-- What a play was started from (a Daily Mix, a mood radio…), for the playlist statistics.
ALTER TABLE play_history ADD COLUMN context TEXT;
CREATE INDEX play_history_context ON play_history(context, started_at) WHERE context IS NOT NULL;

-- Soundprint (MUSIC-1): what each track is like (sad, danceable, sung…) and its language,
-- and a second sound model's embedding while it's tried before switching to it.
ALTER TABLE soundprint ADD COLUMN traits TEXT;          -- JSON, 0–1 each
ALTER TABLE soundprint ADD COLUMN traits_model TEXT;
ALTER TABLE soundprint ADD COLUMN language TEXT;        -- ISO 639-1, from the lyrics; '' = none or unsure
ALTER TABLE soundprint ADD COLUMN next_model TEXT;
ALTER TABLE soundprint ADD COLUMN next_embedding BLOB;

-- +goose Down
ALTER TABLE soundprint DROP COLUMN next_embedding;
ALTER TABLE soundprint DROP COLUMN next_model;
ALTER TABLE soundprint DROP COLUMN language;
ALTER TABLE soundprint DROP COLUMN traits_model;
ALTER TABLE soundprint DROP COLUMN traits;
DROP INDEX play_history_context;
ALTER TABLE play_history DROP COLUMN context;
DROP TABLE music_coplays;
DROP TABLE music_community_checked;
DROP TABLE music_tags;
DROP TABLE music_similar_artists;
