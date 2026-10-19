-- +goose Up
-- The playback check of new video files (Check new files): when a file was last checked
-- (NULL = not yet) and why it failed (NULL = it passed). A scan that finds a file changed
-- clears both, so the file is checked again. Files already in the library count as checked
-- now: only files added from here on are checked.
ALTER TABLE media_files ADD COLUMN checked_at TEXT;
ALTER TABLE media_files ADD COLUMN check_error TEXT;
UPDATE media_files SET checked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');
CREATE INDEX media_files_unchecked ON media_files(id) WHERE checked_at IS NULL;

-- +goose Down
DROP INDEX media_files_unchecked;
ALTER TABLE media_files DROP COLUMN check_error;
ALTER TABLE media_files DROP COLUMN checked_at;
