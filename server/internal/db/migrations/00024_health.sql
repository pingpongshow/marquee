-- +goose Up
-- Library health (ADM-11): items an admin chose to stop seeing under one check.
CREATE TABLE health_ignored (
    check_id TEXT NOT NULL,
    item_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    PRIMARY KEY (check_id, item_id)
);
CREATE INDEX health_ignored_item ON health_ignored(item_id);

-- Playback failures apps report (progress with state=error), kept for 30 days so the
-- playbackErrors check can list titles that won't play.
CREATE TABLE playback_errors (
    id      INTEGER PRIMARY KEY,
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    file_id INTEGER REFERENCES media_files(id) ON DELETE SET NULL,
    at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX playback_errors_item ON playback_errors(item_id, at);
CREATE INDEX playback_errors_at ON playback_errors(at);

-- +goose Down
DROP TABLE playback_errors;
DROP TABLE health_ignored;
