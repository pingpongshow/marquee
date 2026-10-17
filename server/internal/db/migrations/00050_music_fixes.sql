-- +goose Up
-- Fixing music by hand, without editing files.
--
-- An admin's choice of MusicBrainz artist (artist MBID) or album (release group MBID). Keyed
-- like the scanner keys artists and albums, so the choice outlasts the item: an artist whose
-- files all go and come back gets the same match. It wins over MBIDs in tags.
CREATE TABLE music_matches (
    library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('artist', 'album')),
    scan_key   TEXT NOT NULL,
    mbid       TEXT NOT NULL,
    matched_at TEXT NOT NULL,
    PRIMARY KEY (library_id, kind, scan_key)
);

-- A track moved to another album (or album artist) by an admin. Keyed by the media file's
-- path, which a scan moves along when it sees the file moved or its folder renamed; scans
-- place a file found again at that path the same way. album_id is the album picked from
-- the library; the names are used when that album is gone.
CREATE TABLE track_placements (
    path         TEXT PRIMARY KEY,
    library_id   INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    album_id     INTEGER REFERENCES items(id) ON DELETE SET NULL,
    album_artist TEXT NOT NULL,
    album        TEXT NOT NULL,
    artist       TEXT,
    placed_at    TEXT NOT NULL
);

-- +goose Down
DROP TABLE track_placements;
DROP TABLE music_matches;
