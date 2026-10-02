-- +goose Up
-- Intro and credits detection (PLAY-12): which episode files have been checked, and
-- whether anything was found. Detected markers go in markers with source 'detected'.
CREATE TABLE marker_scans (
    file_id    INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    mtime      INTEGER NOT NULL,
    intro      INTEGER NOT NULL DEFAULT 0,
    credits    INTEGER NOT NULL DEFAULT 0,
    scanned_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE marker_scans;
