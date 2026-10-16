-- +goose Up
-- Saving films and episodes on demand to folders besides the default Movies and Shows ones
-- (LIVE-6): the save folder chosen for a save (name and path; '' = the default folder).
ALTER TABLE vod_saves ADD COLUMN destination_name TEXT NOT NULL DEFAULT '';
ALTER TABLE vod_saves ADD COLUMN destination_folder TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE vod_saves DROP COLUMN destination_folder;
ALTER TABLE vod_saves DROP COLUMN destination_name;
