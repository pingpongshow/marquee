-- +goose Up
-- Trickplay also makes an all-keyframe MP4 for Apple players' scrubbing thumbnails (HLS
-- I-frame playlists); files made before it existed are made again.
ALTER TABLE trickplay ADD COLUMN iframes INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE trickplay DROP COLUMN iframes;
