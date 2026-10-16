-- +goose Up
-- Generated episode and video thumbnails: a frame from the file (source 'generated', the
-- image in the image cache) for items without a still. A real still (TMDB, a local file,
-- Plex, an upload) always wins: adding one removes the generated thumb, taking over its
-- selection. Files a frame couldn't be taken from are remembered (by mtime) so the nightly
-- task doesn't try them again until they change.
CREATE TABLE generated_thumb_failures (
    file_id INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    mtime   INTEGER NOT NULL,
    error   TEXT NOT NULL,
    at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose StatementBegin
CREATE TRIGGER artwork_real_thumb_wins AFTER INSERT ON artwork
WHEN NEW.kind = 'thumb' AND NEW.source NOT IN ('generated', 'frame')
BEGIN
    UPDATE artwork SET selected = 1 WHERE id = NEW.id
        AND EXISTS (SELECT 1 FROM artwork WHERE item_id = NEW.item_id AND kind = 'thumb' AND source = 'generated' AND selected = 1);
    DELETE FROM artwork WHERE item_id = NEW.item_id AND kind = 'thumb' AND source = 'generated';
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER artwork_real_thumb_wins;
DROP TABLE generated_thumb_failures;
