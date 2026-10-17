-- +goose Up
-- The optimizer deletes an original once the new file is verified and swapped in, unless
-- optimize.keepOriginals is on (then it goes to the media trash as before).
ALTER TABLE optimize_jobs ADD COLUMN original_deleted INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE optimize_jobs DROP COLUMN original_deleted;
