-- +goose Up
-- Files ffprobe couldn't read. They're skipped on later scans until their size or
-- modification time changes, instead of being re-probed and re-logged every scan.
CREATE TABLE probe_failures (
    path      TEXT PRIMARY KEY,
    library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    size      INTEGER NOT NULL,
    mtime     INTEGER NOT NULL,
    error     TEXT NOT NULL,
    failed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
SELECT 'irreversible';
