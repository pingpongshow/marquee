-- +goose Up
-- A track Soundprint itself couldn't hear (an error for that one track, not the whole run) is
-- tried again on later runs (PLAY-19), but only a few times: failures counts the tries while its
-- file and the model stay the same, and the track is left alone once it reaches the limit.
-- 0 for a track that was heard, whether or not a language was clear.
ALTER TABLE spoken_languages ADD COLUMN failures INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE spoken_languages DROP COLUMN failures;
