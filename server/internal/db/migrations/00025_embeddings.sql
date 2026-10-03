-- +goose Up
-- Text embeddings of movies and shows (USER-15, USER-16): Muse for movies, recommendations
-- and related items. vector is little-endian float32, unit length; text_hash is the SHA-256
-- of the embedded text so changed metadata (or a new model) is re-embedded.
CREATE TABLE item_embeddings (
    item_id   INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    model     TEXT NOT NULL,
    vector    BLOB NOT NULL,
    text_hash TEXT NOT NULL,
    embedded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE item_embeddings;
