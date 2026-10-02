-- +goose Up
-- Initial Marquee schema. See docs/03-architecture.md "Core data model".
-- Released migrations are never edited; add a new numbered file instead.

-- Key/value store for server settings (value is JSON) and server identity.
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- ---------- Users & devices ----------
CREATE TABLE users (
    id                 INTEGER PRIMARY KEY,
    username           TEXT NOT NULL UNIQUE COLLATE NOCASE,
    display_name       TEXT NOT NULL,
    password_hash      TEXT,                 -- NULL for managed profiles that use only a PIN
    pin_hash           TEXT,
    is_admin           INTEGER NOT NULL DEFAULT 0,
    is_managed         INTEGER NOT NULL DEFAULT 0,
    restrictions       TEXT NOT NULL DEFAULT '{}', -- JSON: library ids, max content rating, remote quality cap
    preferences        TEXT NOT NULL DEFAULT '{}', -- JSON: languages, subtitle style, quality defaults
    plex_account_id    INTEGER,                    -- set when imported from Plex
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE devices (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    platform     TEXT NOT NULL,
    product      TEXT NOT NULL DEFAULT '',
    version      TEXT NOT NULL DEFAULT '',
    token_hash   TEXT NOT NULL UNIQUE,  -- SHA-256 of the bearer token; the token itself is never stored
    last_ip      TEXT NOT NULL DEFAULT '',
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (user_id, client_id)
);

-- ---------- Libraries ----------
CREATE TABLE libraries (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL CHECK (type IN ('movies','shows','anime','music','videos','photos')),
    options         TEXT NOT NULL DEFAULT '{}', -- JSON LibraryOptions
    sort_order      INTEGER NOT NULL DEFAULT 0,
    plex_section_id INTEGER,
    last_scanned_at TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE library_paths (
    library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    PRIMARY KEY (library_id, path)
);

-- ---------- Metadata tree ----------
-- One polymorphic table, like Plex's metadata_items: movie, show, season, episode,
-- artist, album, track, video, collection, photo_album, photo.
CREATE TABLE items (
    id               INTEGER PRIMARY KEY,
    library_id       INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    type             TEXT NOT NULL,
    parent_id        INTEGER REFERENCES items(id) ON DELETE CASCADE,
    grandparent_id   INTEGER REFERENCES items(id) ON DELETE CASCADE,
    idx              INTEGER,          -- season/episode/track/disc number
    absolute_idx     INTEGER,          -- anime absolute episode number
    title            TEXT NOT NULL,
    sort_title       TEXT NOT NULL,
    original_title   TEXT,
    tagline          TEXT,
    summary          TEXT,
    year             INTEGER,
    originally_available_at TEXT,      -- release / air date
    content_rating   TEXT,
    studio           TEXT,
    duration_ms      INTEGER,
    critic_rating    REAL,
    audience_rating  REAL,
    child_count      INTEGER NOT NULL DEFAULT 0,
    leaf_count       INTEGER NOT NULL DEFAULT 0,
    extra_type       TEXT,             -- trailer, featurette, behind_the_scenes, theme… (NULL = not an extra)
    locked_fields    TEXT NOT NULL DEFAULT '[]', -- JSON array of field names the agents must not overwrite
    match_state      TEXT NOT NULL DEFAULT 'unmatched' CHECK (match_state IN ('unmatched','matched','local','failed')),
    available        INTEGER NOT NULL DEFAULT 1, -- 0 when all files are missing (offline drive)
    plex_rating_key  INTEGER,
    added_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    metadata_refreshed_at TEXT
);
CREATE INDEX items_library_type ON items(library_id, type, sort_title);
CREATE INDEX items_parent ON items(parent_id, idx);
CREATE INDEX items_grandparent ON items(grandparent_id);
CREATE INDEX items_added ON items(library_id, added_at DESC);
CREATE INDEX items_plex ON items(plex_rating_key);

CREATE TABLE external_ids (
    item_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,   -- tmdb, tvdb, imdb, anidb, anilist, musicbrainz, plex
    value    TEXT NOT NULL,
    PRIMARY KEY (item_id, provider)
);
CREATE INDEX external_ids_lookup ON external_ids(provider, value);

-- Full-text search over titles and people.
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

-- ---------- Media files & streams ----------
CREATE TABLE media_versions (
    id         INTEGER PRIMARY KEY,
    item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    label      TEXT NOT NULL DEFAULT '',  -- edition / version name, e.g. "Director's Cut", "4K"
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX media_versions_item ON media_versions(item_id);

CREATE TABLE media_files (
    id           INTEGER PRIMARY KEY,
    version_id   INTEGER NOT NULL REFERENCES media_versions(id) ON DELETE CASCADE,
    library_id   INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path         TEXT NOT NULL UNIQUE,
    size         INTEGER NOT NULL,
    mtime        INTEGER NOT NULL,       -- unix seconds
    fingerprint  TEXT,                   -- partial-content hash for rename/move detection
    container    TEXT,
    duration_ms  INTEGER,
    bitrate_kbps INTEGER,
    width        INTEGER,
    height       INTEGER,
    video_codec  TEXT,
    audio_codec  TEXT,
    hdr_format   TEXT,                   -- NULL, hdr10, hdr10plus, hlg, dolby_vision
    dv_profile   INTEGER,
    part_index   INTEGER NOT NULL DEFAULT 0, -- stacked multi-part files (cd1/cd2)
    available    INTEGER NOT NULL DEFAULT 1,
    probed_at    TEXT,
    probe_json   TEXT,                   -- raw ffprobe output for diagnostics
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX media_files_version ON media_files(version_id);
CREATE INDEX media_files_fingerprint ON media_files(fingerprint);

CREATE TABLE streams (
    id            INTEGER PRIMARY KEY,
    file_id       INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    stream_index  INTEGER,               -- index within the container; NULL for external subtitle files
    kind          TEXT NOT NULL CHECK (kind IN ('video','audio','subtitle')),
    codec         TEXT NOT NULL,
    profile       TEXT,
    level         INTEGER,
    language      TEXT,
    title         TEXT,
    is_default    INTEGER NOT NULL DEFAULT 0,
    is_forced     INTEGER NOT NULL DEFAULT 0,
    is_hearing_impaired INTEGER NOT NULL DEFAULT 0,
    channels      INTEGER,
    channel_layout TEXT,
    sample_rate   INTEGER,
    bitrate_kbps  INTEGER,
    width         INTEGER,
    height        INTEGER,
    frame_rate    REAL,
    bit_depth     INTEGER,
    color_transfer TEXT,
    color_primaries TEXT,
    external_path TEXT                   -- sidecar subtitle file
);
CREATE INDEX streams_file ON streams(file_id, kind);

-- Chapters and intro/credits markers.
CREATE TABLE markers (
    id        INTEGER PRIMARY KEY,
    file_id   INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    kind      TEXT NOT NULL CHECK (kind IN ('chapter','intro','credits')),
    title     TEXT,
    start_ms  INTEGER NOT NULL,
    end_ms    INTEGER NOT NULL,
    source    TEXT NOT NULL DEFAULT 'file' -- file, detected, plex, manual
);
CREATE INDEX markers_file ON markers(file_id, kind);

-- ---------- People, tags, artwork ----------
CREATE TABLE people (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    tmdb_id    INTEGER UNIQUE,
    photo_path TEXT
);
CREATE INDEX people_name ON people(name);

CREATE TABLE credits (
    item_id   INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    role      TEXT NOT NULL,   -- actor, director, writer, producer, composer…
    character TEXT,
    ord       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, person_id, role)
);
CREATE INDEX credits_person ON credits(person_id);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    kind TEXT NOT NULL,         -- genre, studio, country, label, mood, style
    name TEXT NOT NULL,
    UNIQUE (kind, name)
);

CREATE TABLE item_tags (
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, tag_id)
);
CREATE INDEX item_tags_tag ON item_tags(tag_id);

-- Collections are items of type 'collection'; membership is many-to-many.
CREATE TABLE collection_items (
    collection_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    item_id       INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    ord           INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (collection_id, item_id)
);

CREATE TABLE artwork (
    id         INTEGER PRIMARY KEY,
    item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('poster','backdrop','logo','thumb','banner','clearart')),
    source     TEXT NOT NULL,     -- local, embedded, tmdb, fanart, plex, upload, generated
    remote_url TEXT,
    local_path TEXT,              -- relative to the image cache dir
    width      INTEGER,
    height     INTEGER,
    language   TEXT,
    selected   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX artwork_item ON artwork(item_id, kind, selected);

-- ---------- Per-user state ----------
CREATE TABLE user_item_state (
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id        INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    view_offset_ms INTEGER NOT NULL DEFAULT 0,
    play_count     INTEGER NOT NULL DEFAULT 0,
    last_viewed_at TEXT,
    rating         REAL,
    watchlisted_at TEXT,
    audio_language TEXT,          -- remembered per show/item
    subtitle_language TEXT,
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX user_item_state_recent ON user_item_state(user_id, last_viewed_at DESC);

CREATE TABLE playlists (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('video','audio','photo')),
    smart_rule TEXT,              -- JSON filter for smart playlists; NULL for manual
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE playlist_items (
    id          INTEGER PRIMARY KEY,
    playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    item_id     INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    ord         REAL NOT NULL        -- fractional ordering for cheap reorders
);
CREATE INDEX playlist_items_order ON playlist_items(playlist_id, ord);

-- ---------- Activity ----------
CREATE TABLE play_history (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER REFERENCES users(id) ON DELETE SET NULL,
    item_id       INTEGER REFERENCES items(id) ON DELETE SET NULL,
    device_id     INTEGER REFERENCES devices(id) ON DELETE SET NULL,
    item_title    TEXT NOT NULL,     -- denormalized so history survives item deletion
    started_at    TEXT NOT NULL,
    stopped_at    TEXT,
    position_ms   INTEGER,
    network_class TEXT,              -- local, remote
    decision      TEXT,              -- direct_play, direct_stream, transcode
    video_encoder TEXT,              -- nvenc, qsv, software
    bitrate_kbps  INTEGER,
    source        TEXT NOT NULL DEFAULT 'marquee' -- marquee, plex (imported)
);
CREATE INDEX play_history_user ON play_history(user_id, started_at DESC);
CREATE INDEX play_history_started ON play_history(started_at DESC);

CREATE TABLE task_runs (
    id          INTEGER PRIMARY KEY,
    task        TEXT NOT NULL,
    library_id  INTEGER REFERENCES libraries(id) ON DELETE CASCADE,
    status      TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','cancelled')),
    progress    REAL NOT NULL DEFAULT 0,
    message     TEXT,
    started_at  TEXT,
    finished_at TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX task_runs_recent ON task_runs(task, created_at DESC);

CREATE TABLE webhooks (
    id         INTEGER PRIMARY KEY,
    url        TEXT NOT NULL,
    events     TEXT NOT NULL DEFAULT '[]', -- JSON array
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
-- Down migrations are intentionally unsupported; restore from the automatic pre-migration backup.
SELECT 'irreversible';
