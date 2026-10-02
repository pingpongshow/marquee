-- +goose Up
-- MusicBrainz enrichment (META-3): album release types, and which items have been looked up.
ALTER TABLE items ADD COLUMN release_type TEXT; -- albums: album, ep, single, compilation, live, soundtrack, remix, demo, other
CREATE TABLE musicbrainz_lookups (
    item_id    INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    checked_at TEXT NOT NULL,
    found      INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE musicbrainz_lookups;
ALTER TABLE items DROP COLUMN release_type;
