-- +goose Up
-- Keyframe times of video files, for keyframe-aligned HLS when video is copied (D48).
-- Kept until the file's size or modification time changes.
CREATE TABLE keyframes (
    file_id INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    size    INTEGER NOT NULL,
    mtime   INTEGER NOT NULL,
    times   TEXT NOT NULL -- JSON array of seconds
);

-- +goose Down
DROP TABLE keyframes;
