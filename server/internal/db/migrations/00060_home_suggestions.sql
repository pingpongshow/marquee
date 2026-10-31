-- +goose Up
-- Home suggestions (USER-16).
--
-- Not interested: movies and shows a person asked not to be suggested. They leave every
-- suggestion row and count as something they turned away from.
CREATE TABLE item_not_interested (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    at      TEXT NOT NULL,
    PRIMARY KEY (user_id, item_id)
);

-- How many days a title has been on someone's Home suggestion rows without them opening or
-- playing it; titles passed over day after day slowly give way to others.
CREATE TABLE home_seen (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    days     INTEGER NOT NULL DEFAULT 1,
    last_day TEXT NOT NULL,             -- YYYY-MM-DD
    PRIMARY KEY (user_id, item_id)
);

-- When a title was last shown in each kind of Home row (row_id: the row, or its family for rows
-- that come several at a time, e.g. because-you-watched), so a play soon after counts for
-- that row (play_history.context home:{row}) in Statistics and in ordering their rows.
CREATE TABLE home_shown (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    row_id   TEXT NOT NULL,
    shown_at TEXT NOT NULL,
    PRIMARY KEY (user_id, item_id, row_id)
);
CREATE INDEX home_shown_at ON home_shown(shown_at);

-- +goose Down
DROP INDEX home_shown_at;
DROP TABLE home_shown;
DROP TABLE home_seen;
DROP TABLE item_not_interested;
