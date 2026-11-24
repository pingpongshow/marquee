-- +goose Up
-- Collections of one's own (USER-23): a video playlist becomes a list of titles from one video
-- library, kept under that library. The ones made before are sorted into libraries by the
-- server when it starts (items.ConvertVideoPlaylists), since smart ones need working out.
ALTER TABLE playlists ADD COLUMN library_id INTEGER REFERENCES libraries(id) ON DELETE CASCADE;
CREATE INDEX playlists_library ON playlists(library_id) WHERE library_id IS NOT NULL;

-- +goose Down
DROP INDEX playlists_library;
ALTER TABLE playlists DROP COLUMN library_id;
