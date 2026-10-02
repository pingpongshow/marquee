-- +goose Up
-- Seek-bar preview sprite sheets (PLAY-13). Images live in <cache>/trickplay/<file_id>/;
-- a row with an error means generation failed for this version of the file (not retried
-- until the file changes).
CREATE TABLE trickplay (
    file_id      INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    mtime        INTEGER NOT NULL,   -- media_files.mtime when generated
    interval_ms  INTEGER NOT NULL,
    width        INTEGER NOT NULL,
    height       INTEGER NOT NULL,
    count        INTEGER NOT NULL,   -- thumbnails (sheets hold 10×10)
    error        TEXT,
    generated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE trickplay;
