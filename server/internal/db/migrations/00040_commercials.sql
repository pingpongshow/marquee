-- +goose Up
-- Commercials in Live TV recordings (LIVE-5), per media file: found with Comskip and, when
-- the admin wants, cut out. A scan row per handled file: its mtime when handled (a file that
-- changed otherwise is looked at again), the outcome, why it failed, how much was cut and
-- where the original went in the media trash (to restore it). commercial_breaks holds the
-- breaks found, or once cut, what was actually removed (original timeline).
CREATE TABLE commercial_scans (
    file_id    INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    mtime      INTEGER NOT NULL,
    status     TEXT NOT NULL,              -- queued, running, detected, waiting, cut, restored, failed
    error      TEXT NOT NULL DEFAULT '',
    breaks     INTEGER NOT NULL DEFAULT 0,
    removed_ms INTEGER NOT NULL DEFAULT 0,
    original   TEXT NOT NULL DEFAULT '',   -- the uncut recording in the media trash
    scanned_at TEXT
);

CREATE TABLE commercial_breaks (
    file_id  INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    start_ms INTEGER NOT NULL,
    end_ms   INTEGER NOT NULL,
    PRIMARY KEY (file_id, start_ms)
);

-- +goose Down
DROP TABLE commercial_breaks;
DROP TABLE commercial_scans;
