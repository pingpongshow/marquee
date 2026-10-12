-- +goose Up
-- Lyric theme index (optional AI): the embedding of each track's lyrics (the first ~400
-- tokens) with the configured Ollama embedding model, so music Muse can match prompts like
-- "songs about the ocean". vector is little-endian float32, unit length; text_hash is the
-- SHA-256 of the embedded text, so changed lyrics (or a new model) are re-embedded.
CREATE TABLE lyric_embeddings (
    item_id     INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    model       TEXT NOT NULL,
    vector      BLOB NOT NULL,
    text_hash   TEXT NOT NULL,
    embedded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE lyric_embeddings;
