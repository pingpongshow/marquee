-- +goose Up
-- The language of subtitle tracks whose file doesn't say, read from their text (PLAY-20). Kept
-- apart from streams, which a rescan writes again: an embedded track ("s:<index>") stays with
-- its video file, a subtitle file ("x:<path>") with itself, while its size and time are the
-- same. language is NULL when the text didn't say clearly.
CREATE TABLE subtitle_languages (
    file_id    INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    stream_key TEXT NOT NULL,
    size       INTEGER NOT NULL,
    mtime      INTEGER NOT NULL,
    language   TEXT,
    model      TEXT NOT NULL,
    checked_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (file_id, stream_key)
);

-- +goose Down
DROP TABLE subtitle_languages;
