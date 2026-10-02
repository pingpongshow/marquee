-- +goose Up
-- Sonic analysis per track (MUSIC-1, D56): a CLAP embedding (float32 little-endian) plus
-- tempo, key and energy. Re-analysed when the file or the model changes.
CREATE TABLE sonic (
    item_id     INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    file_id     INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    model       TEXT NOT NULL,
    embedding   BLOB,
    bpm         REAL,
    musical_key TEXT,
    mode        TEXT,
    energy      REAL,
    error       TEXT,          -- analysis failed (kept so the file isn't retried every run)
    analyzed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Loudness levelling (MUSIC-9): ReplayGain-style gains in dB relative to -18 LUFS.
ALTER TABLE media_files ADD COLUMN track_gain_db REAL;
ALTER TABLE media_files ADD COLUMN album_gain_db REAL;
ALTER TABLE media_files ADD COLUMN track_peak REAL;
ALTER TABLE media_files ADD COLUMN loudness_source TEXT; -- tags, analysis

-- Lyrics (MUSIC-10): plain or LRC-timed text from tags, sidecars or LRCLIB.
CREATE TABLE lyrics (
    item_id    INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    synced     INTEGER NOT NULL DEFAULT 0,
    text       TEXT NOT NULL,     -- empty = looked up, none found
    source     TEXT NOT NULL,     -- embedded, sidecar, lrclib
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE lyrics;
ALTER TABLE media_files DROP COLUMN loudness_source;
ALTER TABLE media_files DROP COLUMN track_peak;
ALTER TABLE media_files DROP COLUMN album_gain_db;
ALTER TABLE media_files DROP COLUMN track_gain_db;
DROP TABLE sonic;
