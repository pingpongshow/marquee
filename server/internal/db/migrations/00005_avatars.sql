-- +goose Up
-- Profile pictures live in <config>/avatars/<user id>.jpg; the version busts client caches (0 = none).
ALTER TABLE users ADD COLUMN avatar_version INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN avatar_version;
