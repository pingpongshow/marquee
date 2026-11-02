-- +goose Up
-- Playlists someone has shared (MUSIC-24). shared_with_all offers one to everyone, now and
-- later (admins only). A row is one person's answer: pending until they accept; removed marks
-- someone who took themselves off a playlist shared with everyone, so it doesn't come back
-- until the owner shares it with them again.
ALTER TABLE playlists ADD COLUMN shared_with_all INTEGER NOT NULL DEFAULT 0;
CREATE TABLE playlist_shares (
    playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status      TEXT NOT NULL CHECK (status IN ('pending', 'accepted', 'removed')),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (playlist_id, user_id)
);
CREATE INDEX playlist_shares_user ON playlist_shares(user_id, status);

-- +goose Down
DROP TABLE playlist_shares;
ALTER TABLE playlists DROP COLUMN shared_with_all;
