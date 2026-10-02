-- +goose Up
-- Subtitles downloaded from OpenSubtitles (PLAY-7). Media folders are read-only, so the
-- files live under the config directory; the scanner re-attaches them as external
-- subtitle streams whenever it rewrites a file's streams.
CREATE TABLE downloaded_subtitles (
    id               INTEGER PRIMARY KEY,
    file_id          INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    path             TEXT NOT NULL UNIQUE,
    language         TEXT,
    title            TEXT,
    hearing_impaired INTEGER NOT NULL DEFAULT 0,
    provider         TEXT NOT NULL DEFAULT 'opensubtitles',
    provider_id      TEXT,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX downloaded_subtitles_file ON downloaded_subtitles(file_id);

-- +goose Down
DROP TABLE downloaded_subtitles;
