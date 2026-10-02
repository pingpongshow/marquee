-- +goose Up
-- AniList (META-2): other titles anime is known by (romaji, English, synonyms) are searchable,
-- and its AniList score is kept.
ALTER TABLE items ADD COLUMN alt_titles TEXT;      -- newline-separated
ALTER TABLE items ADD COLUMN anilist_score INTEGER; -- 0–100
CREATE TABLE anilist_lookups (
    item_id    INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    checked_at TEXT NOT NULL,
    found      INTEGER NOT NULL DEFAULT 0
);

DROP TRIGGER items_fts_ai;
DROP TRIGGER items_fts_ad;
DROP TRIGGER items_fts_au;
DROP TABLE items_fts;
CREATE VIRTUAL TABLE items_fts USING fts5(
    title, original_title, sort_title, alt_titles,
    content='items', content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);
-- +goose StatementBegin
CREATE TRIGGER items_fts_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, original_title, sort_title, alt_titles) VALUES (new.id, new.title, new.original_title, new.sort_title, new.alt_titles);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER items_fts_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title, sort_title, alt_titles) VALUES ('delete', old.id, old.title, old.original_title, old.sort_title, old.alt_titles);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER items_fts_au AFTER UPDATE OF title, original_title, sort_title, alt_titles ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title, sort_title, alt_titles) VALUES ('delete', old.id, old.title, old.original_title, old.sort_title, old.alt_titles);
    INSERT INTO items_fts(rowid, title, original_title, sort_title, alt_titles) VALUES (new.id, new.title, new.original_title, new.sort_title, new.alt_titles);
END;
-- +goose StatementEnd
INSERT INTO items_fts(items_fts) VALUES ('rebuild');

-- +goose Down
DROP TRIGGER items_fts_ai;
DROP TRIGGER items_fts_ad;
DROP TRIGGER items_fts_au;
DROP TABLE items_fts;
CREATE VIRTUAL TABLE items_fts USING fts5(
    title, original_title, sort_title,
    content='items', content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);
-- +goose StatementBegin
CREATE TRIGGER items_fts_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, original_title, sort_title) VALUES (new.id, new.title, new.original_title, new.sort_title);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER items_fts_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title, sort_title) VALUES ('delete', old.id, old.title, old.original_title, old.sort_title);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER items_fts_au AFTER UPDATE OF title, original_title, sort_title ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title, sort_title) VALUES ('delete', old.id, old.title, old.original_title, old.sort_title);
    INSERT INTO items_fts(rowid, title, original_title, sort_title) VALUES (new.id, new.title, new.original_title, new.sort_title);
END;
-- +goose StatementEnd
INSERT INTO items_fts(items_fts) VALUES ('rebuild');
DROP TABLE anilist_lookups;
ALTER TABLE items DROP COLUMN anilist_score;
ALTER TABLE items DROP COLUMN alt_titles;
