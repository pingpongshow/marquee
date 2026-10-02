-- +goose Up
-- Scanner support (M1).

-- Stable identity derived from file/folder names (or tags for music), used to group
-- files into the same item across rescans even after metadata matching renames the title.
ALTER TABLE items ADD COLUMN scan_key TEXT;
CREATE INDEX items_scan_key ON items(library_id, type, scan_key);

-- Music: disc number, and the per-track artist credit (may differ from the album artist).
ALTER TABLE items ADD COLUMN disc INTEGER;
ALTER TABLE items ADD COLUMN artist_credit TEXT;

-- Sidecar files that belong to a media file but aren't streams (lyrics, local artwork, nfo).
CREATE TABLE sidecars (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    kind    TEXT NOT NULL,   -- lyrics, nfo
    path    TEXT NOT NULL UNIQUE
);
CREATE INDEX sidecars_file ON sidecars(file_id);

-- Items that only exist because of files (seasons, albums…) are removed when empty.
CREATE INDEX media_files_library ON media_files(library_id, available);

-- +goose Down
SELECT 'irreversible';
