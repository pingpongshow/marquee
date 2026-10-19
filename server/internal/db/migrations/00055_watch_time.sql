-- +goose Up
-- Time limits for kids' profiles: minutes of video each person watched per day (the
-- server's local date, YYYY-MM-DD), counted while it plays.
CREATE TABLE watch_time (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day     TEXT NOT NULL,
    ms      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);

-- +goose Down
DROP TABLE watch_time;
