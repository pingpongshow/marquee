-- +goose Up
-- When each kind of per-person change was last made (USER-18), so a change an app made
-- offline and sends later doesn't overwrite a newer one made elsewhere.
CREATE TABLE change_clock (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    field   TEXT NOT NULL, -- rating, watched, watchlist, comment
    at      TEXT NOT NULL,
    PRIMARY KEY (user_id, item_id, field)
);

-- +goose Down
DROP TABLE change_clock;
