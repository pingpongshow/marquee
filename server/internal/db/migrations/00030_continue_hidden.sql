-- +goose Up
-- Titles a person removed from Continue Watching (USER-6): a movie or video by its own id,
-- an episode by its show's id. Hidden until watched again after hidden_at.
CREATE TABLE continue_hidden (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id   INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    hidden_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (user_id, item_id)
);

-- +goose Down
DROP TABLE continue_hidden;
