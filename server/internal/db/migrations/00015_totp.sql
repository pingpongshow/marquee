-- +goose Up
-- Two-factor sign-in (TOTP, RFC 6238) for password sign-ins.
CREATE TABLE user_totp (
    user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret     TEXT,                       -- base32; set once enabled
    pending    TEXT,                       -- base32; during setup
    recovery   TEXT NOT NULL DEFAULT '[]', -- JSON array of SHA-256 hashes of unused recovery codes
    last_step  INTEGER NOT NULL DEFAULT 0, -- the last accepted time step (no replays)
    enabled_at TEXT
);

-- +goose Down
DROP TABLE user_totp;
