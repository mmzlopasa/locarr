-- +goose Up
-- P4: the shared library core (items, files, timeline, wanted state, recycle
-- bin, metadata cache) and movies. Series (P5) and music (P6) add their own
-- detail tables next to `movies` and reuse everything else.

-- One row per library item of any kind. Columns every kind shares live here;
-- kind-specific facts live in the kind's own table keyed by item_id.
CREATE TABLE media_items (
    id                   INTEGER PRIMARY KEY,
    kind                 TEXT    NOT NULL CHECK (kind IN ('movie', 'series', 'artist')),
    title                TEXT    NOT NULL,
    sort_title           TEXT    NOT NULL,
    year                 INTEGER NOT NULL DEFAULT 0,
    monitored            INTEGER NOT NULL DEFAULT 1,
    quality_profile_id   INTEGER NOT NULL REFERENCES quality_profiles (id),
    root_folder_id       INTEGER REFERENCES root_folders (id) ON DELETE SET NULL,
    path                 TEXT    NOT NULL,   -- absolute folder of the item
    tags                 TEXT    NOT NULL DEFAULT '[]', -- JSON array of lower-case tags (D-027)
    added_at             INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL,
    UNIQUE (path)
);
CREATE INDEX media_items_kind_sort ON media_items (kind, sort_title, id);
CREATE INDEX media_items_kind_added ON media_items (kind, added_at);
CREATE INDEX media_items_profile ON media_items (quality_profile_id);

CREATE TABLE movies (
    item_id               INTEGER PRIMARY KEY REFERENCES media_items (id) ON DELETE CASCADE,
    tmdb_id               INTEGER NOT NULL UNIQUE,
    imdb_id               TEXT    NOT NULL DEFAULT '',
    original_title        TEXT    NOT NULL DEFAULT '',
    original_language     TEXT    NOT NULL DEFAULT '',
    alternate_titles      TEXT    NOT NULL DEFAULT '[]',
    overview              TEXT    NOT NULL DEFAULT '',
    runtime               INTEGER NOT NULL DEFAULT 0,  -- minutes
    status                TEXT    NOT NULL DEFAULT '',  -- TMDb: Rumored, Planned, In Production, Post Production, Released, Canceled
    studio                TEXT    NOT NULL DEFAULT '',
    genres                TEXT    NOT NULL DEFAULT '[]',
    certification         TEXT    NOT NULL DEFAULT '',
    rating                REAL    NOT NULL DEFAULT 0,
    votes                 INTEGER NOT NULL DEFAULT 0,
    popularity            REAL    NOT NULL DEFAULT 0,
    in_cinemas            TEXT    NOT NULL DEFAULT '',  -- YYYY-MM-DD
    digital_release       TEXT    NOT NULL DEFAULT '',
    physical_release      TEXT    NOT NULL DEFAULT '',
    collection_tmdb_id    INTEGER NOT NULL DEFAULT 0,
    collection_name       TEXT    NOT NULL DEFAULT '',
    poster                TEXT    NOT NULL DEFAULT '',  -- provider image file name
    backdrop              TEXT    NOT NULL DEFAULT '',
    minimum_availability  TEXT    NOT NULL DEFAULT 'released' CHECK (minimum_availability IN ('announced', 'in_cinemas', 'released')),
    added_by_list_id      INTEGER,
    metadata_refreshed_at INTEGER
);
CREATE INDEX movies_imdb ON movies (imdb_id) WHERE imdb_id <> '';
CREATE INDEX movies_collection ON movies (collection_tmdb_id) WHERE collection_tmdb_id <> 0;
CREATE INDEX movies_in_cinemas ON movies (in_cinemas) WHERE in_cinemas <> '';
CREATE INDEX movies_digital ON movies (digital_release) WHERE digital_release <> '';
CREATE INDEX movies_physical ON movies (physical_release) WHERE physical_release <> '';

-- Files in the library. A movie has at most one; episodes (P5) and tracks
-- (P6) map files through their own tables.
CREATE TABLE media_files (
    id               INTEGER PRIMARY KEY,
    item_id          INTEGER NOT NULL REFERENCES media_items (id) ON DELETE CASCADE,
    path             TEXT    NOT NULL UNIQUE,  -- absolute
    size             INTEGER NOT NULL,
    quality          TEXT    NOT NULL,         -- quality id (internal/quality)
    revision         TEXT    NOT NULL DEFAULT '{"version":1}',
    custom_formats   TEXT    NOT NULL DEFAULT '[]',  -- names of matched formats
    score            INTEGER NOT NULL DEFAULT 0,
    release_group    TEXT    NOT NULL DEFAULT '',
    edition          TEXT    NOT NULL DEFAULT '',
    languages        TEXT    NOT NULL DEFAULT '[]',
    media_info       TEXT    NOT NULL DEFAULT '{}',
    scene_name       TEXT    NOT NULL DEFAULT '',  -- release title it came from
    download_id      INTEGER REFERENCES downloads (id) ON DELETE SET NULL,
    added_at         INTEGER NOT NULL
);
CREATE INDEX media_files_item ON media_files (item_id);

-- Per-title timeline (D-030): what Locarr did for an item and why. Searches
-- and download events are read from search_log and download_events; this
-- table holds everything else.
CREATE TABLE timeline_events (
    id           INTEGER PRIMARY KEY,
    item_ref     TEXT    NOT NULL,   -- "movie:42"
    at           INTEGER NOT NULL,
    type         TEXT    NOT NULL,
    message      TEXT    NOT NULL DEFAULT '',
    data         TEXT    NOT NULL DEFAULT '{}',
    download_id  INTEGER REFERENCES downloads (id) ON DELETE SET NULL
);
CREATE INDEX timeline_events_item ON timeline_events (item_ref, at);
CREATE INDEX timeline_events_at ON timeline_events (at);

-- Downloads carry item_ref as JSON ({"ref": "movie:42"}); the timeline and
-- the explainer look them up by ref.
CREATE INDEX downloads_item_ref ON downloads (json_extract(item_ref, '$.ref'), added_at);

-- Automatic search state per wanted item (backoff between searches).
CREATE TABLE search_state (
    item_ref        TEXT    PRIMARY KEY,
    searches        INTEGER NOT NULL DEFAULT 0,
    last_search_at  INTEGER,
    next_search_at  INTEGER,
    last_result     TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX search_state_next ON search_state (next_search_at);

-- Releases held back by a delay profile (or another temporary rejection),
-- re-evaluated until they are grabbed, superseded or expire.
CREATE TABLE pending_releases (
    id           INTEGER PRIMARY KEY,
    item_ref     TEXT    NOT NULL,
    guid         TEXT    NOT NULL,
    indexer_id   INTEGER NOT NULL DEFAULT 0,
    title        TEXT    NOT NULL,
    candidate    TEXT    NOT NULL,   -- decision.Candidate as JSON
    reason       TEXT    NOT NULL DEFAULT '',
    first_seen   INTEGER NOT NULL,
    retry_at     INTEGER NOT NULL,
    UNIQUE (item_ref, guid, indexer_id)
);
CREATE INDEX pending_releases_retry ON pending_releases (retry_at);

-- Files moved to the recycle bin; restore puts them back, retention empties it.
CREATE TABLE recycle_bin (
    id             INTEGER PRIMARY KEY,
    original_path  TEXT    NOT NULL,
    recycled_path  TEXT    NOT NULL UNIQUE,
    size           INTEGER NOT NULL DEFAULT 0,
    is_dir         INTEGER NOT NULL DEFAULT 0,
    item_ref       TEXT    NOT NULL DEFAULT '',
    reason         TEXT    NOT NULL DEFAULT '',
    recycled_at    INTEGER NOT NULL
);
CREATE INDEX recycle_bin_at ON recycle_bin (recycled_at);

-- Cached metadata provider answers (D-029).
CREATE TABLE metadata_cache (
    key         TEXT    PRIMARY KEY,
    body        BLOB    NOT NULL,
    fetched_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);
CREATE INDEX metadata_cache_expires ON metadata_cache (expires_at);

-- Lists that add movies automatically (TMDb lists and collections for now).
CREATE TABLE import_lists (
    id                    INTEGER PRIMARY KEY,
    name                  TEXT    NOT NULL UNIQUE,
    kind                  TEXT    NOT NULL,   -- tmdb_list, tmdb_collection
    list_id               TEXT    NOT NULL,
    enabled               INTEGER NOT NULL DEFAULT 1,
    root_folder_id        INTEGER NOT NULL REFERENCES root_folders (id),
    quality_profile_id    INTEGER NOT NULL REFERENCES quality_profiles (id),
    minimum_availability  TEXT    NOT NULL DEFAULT 'released',
    monitored             INTEGER NOT NULL DEFAULT 1,
    search_on_add         INTEGER NOT NULL DEFAULT 0,
    tags                  TEXT    NOT NULL DEFAULT '[]',
    last_sync_at          INTEGER,
    last_added            INTEGER NOT NULL DEFAULT 0,
    last_error            TEXT    NOT NULL DEFAULT '',
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);

-- Movies a list must never add again (deleted with "don't add again").
CREATE TABLE import_exclusions (
    tmdb_id     INTEGER PRIMARY KEY,
    title       TEXT    NOT NULL DEFAULT '',
    year        INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);

-- Per-user calendar feed tokens (SHA-256, like API keys).
CREATE TABLE calendar_tokens (
    user_id     INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT    NOT NULL UNIQUE,
    created_at  INTEGER NOT NULL
);

-- +goose Down
DROP TABLE calendar_tokens;
DROP TABLE import_exclusions;
DROP TABLE import_lists;
DROP TABLE metadata_cache;
DROP TABLE recycle_bin;
DROP TABLE pending_releases;
DROP TABLE search_state;
DROP INDEX downloads_item_ref;
DROP TABLE timeline_events;
DROP TABLE media_files;
DROP TABLE movies;
DROP TABLE media_items;
