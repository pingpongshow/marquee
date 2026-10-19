-- +goose Up
-- The storage overview's growth history (Dashboard → Storage): each library's size, one
-- sample a day (the server's local date), kept for a year.
CREATE TABLE storage_samples (
    library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    date       TEXT NOT NULL, -- YYYY-MM-DD
    bytes      INTEGER NOT NULL,
    files      INTEGER NOT NULL,
    PRIMARY KEY (library_id, date)
);
CREATE INDEX storage_samples_date ON storage_samples(date);

-- +goose Down
DROP TABLE storage_samples;
