-- +goose Up
-- The frame timing check (Check files, ADM-11): when a video file's frame timing was checked
-- (NULL = not yet; files already in the library are checked too) and what's wrong with it
-- (NULL = nothing). Some MP4s time their frames in decode order instead of display order and
-- stutter unless converted while playing. A scan that finds a file changed clears both, so
-- the file is checked again.
ALTER TABLE media_files ADD COLUMN timing_checked_at TEXT;
ALTER TABLE media_files ADD COLUMN timing_error TEXT;
CREATE INDEX media_files_timing_unchecked ON media_files(id) WHERE timing_checked_at IS NULL;
CREATE INDEX media_files_timing_error ON media_files(id) WHERE timing_error IS NOT NULL;

-- Repairs asked for in Library Health → Frame timing errors, worked through one at a time.
-- A repaired file leaves the table (and the check); one that failed stays, with why, until
-- it's asked for again.
CREATE TABLE timing_repairs (
    file_id     INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    state       TEXT NOT NULL CHECK (state IN ('queued', 'running', 'failed')),
    progress    REAL NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    finished_at TEXT
);

-- +goose Down
DROP TABLE timing_repairs;
DROP INDEX media_files_timing_error;
DROP INDEX media_files_timing_unchecked;
ALTER TABLE media_files DROP COLUMN timing_error;
ALTER TABLE media_files DROP COLUMN timing_checked_at;
