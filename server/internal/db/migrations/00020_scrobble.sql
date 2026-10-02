-- +goose Up
-- Scrobbling (MUSIC-12): each user's ListenBrainz token.
CREATE TABLE user_scrobble (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    service   TEXT NOT NULL,          -- listenbrainz
    token     TEXT NOT NULL,
    username  TEXT NOT NULL DEFAULT '',
    error     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, service)
);

-- +goose Down
DROP TABLE user_scrobble;
