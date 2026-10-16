-- +goose Up
-- The library optimizer (Library Health → Large files): files re-encoded to save space.
-- One row per queued file. The finished file replaces the original under the same record
-- (so the item keeps its id, watch history and metadata); the original goes to the media
-- trash (original = where it went) and can be put back while it's there.
CREATE TABLE optimize_jobs (
    id          INTEGER PRIMARY KEY,
    file_id     INTEGER REFERENCES media_files(id) ON DELETE SET NULL,
    item_id     INTEGER REFERENCES items(id) ON DELETE SET NULL,
    library_id  INTEGER NOT NULL DEFAULT 0,
    title       TEXT NOT NULL,
    path        TEXT NOT NULL,              -- the original's path
    final_path  TEXT NOT NULL DEFAULT '',   -- the optimized file's path (differs when the container changed)
    preset_id   TEXT NOT NULL,
    preset_name TEXT NOT NULL,
    preset      TEXT NOT NULL,              -- JSON: the preset as queued
    state       TEXT NOT NULL CHECK (state IN ('queued','running','verifying','done','failed','cancelled','skipped','restored')),
    progress    REAL NOT NULL DEFAULT 0,
    encoder     TEXT NOT NULL DEFAULT '',
    before_size INTEGER NOT NULL DEFAULT 0,
    after_size  INTEGER NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT '',   -- why it failed or was skipped
    original    TEXT NOT NULL DEFAULT '',   -- the original in the media trash
    cleared     INTEGER NOT NULL DEFAULT 0, -- hidden from the queue (still in the history)
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at  TEXT,
    finished_at TEXT
);
CREATE INDEX optimize_jobs_state ON optimize_jobs(state, id);
CREATE INDEX optimize_jobs_file ON optimize_jobs(file_id);

-- +goose Down
DROP TABLE optimize_jobs;
