-- +goose Up
-- Track choices remembered per show or movie (PLAY-8) keep more than the language, so the
-- next episode gets the same track: its title (SDH, Commentary, Signs & Songs) and, for
-- subtitles, whether it was the forced track. A track without a language is remembered as
-- 'und' with its title.
ALTER TABLE user_item_state ADD COLUMN audio_title TEXT;
ALTER TABLE user_item_state ADD COLUMN subtitle_title TEXT;
ALTER TABLE user_item_state ADD COLUMN subtitle_forced INTEGER;

-- +goose Down
ALTER TABLE user_item_state DROP COLUMN subtitle_forced;
ALTER TABLE user_item_state DROP COLUMN subtitle_title;
ALTER TABLE user_item_state DROP COLUMN audio_title;
