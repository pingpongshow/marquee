-- +goose Up
-- The Check files task called two kinds of good file damaged: one whose picture stops before
-- its sound, tested past the last frame, and one whose chapter index points at a missing
-- track. Files with those reasons are checked again; a file really cut short is found again.
UPDATE media_files SET checked_at = NULL, check_error = NULL
WHERE check_error = 'The end doesn''t decode: no frames'
	OR check_error LIKE '%Referenced QT chapter track not found';

-- +goose Down
SELECT 1;
