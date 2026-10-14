-- +goose Up
-- Email addresses, password and PIN resets, and admin notifications.
--
-- users.email is optional; when set it is unique ignoring case. It receives reset links and,
-- for administrators, alerts.
ALTER TABLE users ADD COLUMN email TEXT;
CREATE UNIQUE INDEX users_email ON users(email COLLATE NOCASE) WHERE email IS NOT NULL;

-- Single-use reset links: only the SHA-256 of the token is stored. A link is good for 30
-- minutes; using one (or a reset) marks the user's other links used.
CREATE TABLE password_resets (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    kind       TEXT NOT NULL CHECK (kind IN ('password', 'pin')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX password_resets_user ON password_resets(user_id);

-- Alerts for administrators (task failed, disk space low, ...), shown under the bell in the
-- web app. Pruned after 30 days.
CREATE TABLE notifications (
    id         INTEGER PRIMARY KEY,
    type       TEXT NOT NULL,
    subject    TEXT NOT NULL DEFAULT '',
    title      TEXT NOT NULL,
    message    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    read_at    TEXT
);
CREATE INDEX notifications_created ON notifications(created_at);

-- Devices each user has signed in from, kept after sign-out, so an administrator's sign-in
-- from a device never seen before can raise an alert.
CREATE TABLE known_devices (
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id     TEXT NOT NULL,
    first_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (user_id, client_id)
);
INSERT OR IGNORE INTO known_devices (user_id, client_id) SELECT user_id, client_id FROM devices;

-- +goose Down
DROP TABLE known_devices;
DROP TABLE notifications;
DROP TABLE password_resets;
DROP INDEX users_email;
ALTER TABLE users DROP COLUMN email;
