-- +goose Up
-- The language a film's or episode's audio track is spoken in, heard by Soundprint for tracks
-- whose file doesn't say (PLAY-19). Kept apart from streams, which a rescan writes again: a
-- detection stays with the file while its size and time are the same. language is NULL when
-- nothing clear was heard; the stream gets it only when confidence is high enough.
CREATE TABLE spoken_languages (
    file_id      INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    stream_index INTEGER NOT NULL,
    size         INTEGER NOT NULL,
    mtime        INTEGER NOT NULL,
    language     TEXT,
    confidence   REAL NOT NULL DEFAULT 0,
    model        TEXT NOT NULL,
    checked_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (file_id, stream_index)
);
-- A stream whose language was heard rather than read from the file.
ALTER TABLE streams ADD COLUMN language_detected INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE streams DROP COLUMN language_detected;
DROP TABLE spoken_languages;
