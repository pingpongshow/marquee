-- +goose Up
-- Offline downloads (M7): videos converted for phones and tablets. Originals are served
-- directly and need no job.
CREATE TABLE download_jobs (
    id          TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id     INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    file_id     INTEGER,
    quality     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'converting', 'ready', 'failed')),
    progress    REAL NOT NULL DEFAULT 0,
    size        INTEGER,
    path        TEXT,
    error       TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at TEXT
);
CREATE INDEX download_jobs_user ON download_jobs(user_id, created_at DESC);

-- +goose Down
DROP TABLE download_jobs;
