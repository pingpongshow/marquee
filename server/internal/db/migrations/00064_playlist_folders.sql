-- +goose Up
-- Folders someone sorts their playlists into (MUSIC-28). Each person's folders are their own,
-- and hold their own playlists and those shared with them; a playlist sits in one folder at most.
CREATE TABLE playlist_folders (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX playlist_folders_user ON playlist_folders(user_id);
CREATE TABLE playlist_folder_entries (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    folder_id   INTEGER NOT NULL REFERENCES playlist_folders(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, playlist_id)
);
CREATE INDEX playlist_folder_entries_folder ON playlist_folder_entries(folder_id);

-- +goose Down
DROP TABLE playlist_folder_entries;
DROP TABLE playlist_folders;
