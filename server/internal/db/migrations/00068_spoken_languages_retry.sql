-- +goose Up
-- Audio tracks the first spoken-language run couldn't hear (the Soundprint container couldn't
-- see films and episodes, PLAY-19) are heard again: the server now sends the audio itself.
DELETE FROM spoken_languages WHERE language IS NULL;

-- +goose Down
SELECT 1;
