-- +goose Up
-- An artist's most-listened tracks in the library, from ListenBrainz (MUSIC-15).
CREATE TABLE music_popular (
    artist_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    track_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    rank      INTEGER NOT NULL,
    PRIMARY KEY (artist_id, track_id)
);
CREATE INDEX music_popular_rank ON music_popular(artist_id, rank);

-- +goose Down
DROP TABLE music_popular;
