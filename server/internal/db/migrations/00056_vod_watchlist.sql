-- +goose Up
-- Films and series on demand on a person's watchlist (USER-8), next to the library's
-- (user_item_state.watchlisted_at). Kept by the row's id, which survives the source
-- renumbering its catalogue (vod/carry.go); a title that leaves the source leaves the list.
CREATE TABLE vod_watchlist (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL CHECK (kind IN ('movie', 'series')),
    vod_id   INTEGER NOT NULL,
    added_at TEXT NOT NULL,
    PRIMARY KEY (user_id, kind, vod_id)
);
CREATE INDEX vod_watchlist_recent ON vod_watchlist(user_id, added_at DESC);

-- +goose Down
DROP TABLE vod_watchlist;
