-- +goose Up
-- A picture of its own for a playlist or a collection (MUSIC-30), chosen by its owner: an
-- uploaded image, or one downloaded from a link, kept in the image cache.
ALTER TABLE playlists ADD COLUMN cover_path TEXT;

-- +goose Down
ALTER TABLE playlists DROP COLUMN cover_path;
