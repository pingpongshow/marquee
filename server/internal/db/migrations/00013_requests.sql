-- +goose Up
-- Requests for titles that aren't in the library (REQ-1). They wait here for an admin's
-- approval and are then sent to Seerr.
CREATE TABLE media_requests (
    id               INTEGER PRIMARY KEY,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tmdb_id          INTEGER NOT NULL,
    media_type       TEXT NOT NULL CHECK (media_type IN ('movie', 'tv')),
    title            TEXT NOT NULL,
    year             INTEGER,
    poster_path      TEXT,
    seasons          TEXT NOT NULL DEFAULT '[]', -- JSON array of season numbers; [] = all
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'declined', 'failed', 'available')),
    reason           TEXT,
    seerr_request_id INTEGER,
    decided_by       INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    decided_at       TEXT
);
CREATE INDEX media_requests_user ON media_requests(user_id, created_at DESC);
CREATE INDEX media_requests_status ON media_requests(status, created_at);
CREATE INDEX media_requests_tmdb ON media_requests(media_type, tmdb_id);

-- +goose Down
DROP TABLE media_requests;
