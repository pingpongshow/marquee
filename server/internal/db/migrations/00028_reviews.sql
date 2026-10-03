-- +goose Up
-- Comments people leave with their ratings (USER-17); ratings stay in user_item_state.
CREATE TABLE item_reviews (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    comment    TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX item_reviews_item ON item_reviews(item_id);

-- +goose Down
DROP TABLE item_reviews;
