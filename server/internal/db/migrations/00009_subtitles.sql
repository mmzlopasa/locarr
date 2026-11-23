-- +goose Up
-- P7: subtitles. Language profiles, providers with health and quota, the
-- subtitles Locarr knows next to each media file, embedded tracks read from
-- the files, download history, the blacklist and per-item search state.

CREATE TABLE subtitle_profiles (
    id               INTEGER PRIMARY KEY,
    name             TEXT    NOT NULL UNIQUE,
    -- Ordered JSON array of {"language","forced","hearingImpaired"}; forced:
    -- true = only foreign-parts subtitles; hearingImpaired: include, prefer, exclude.
    languages        TEXT    NOT NULL DEFAULT '[]',
    skip_embedded    INTEGER NOT NULL DEFAULT 1,  -- a matching embedded text track counts as present
    cutoff           INTEGER NOT NULL DEFAULT 0,  -- stop once the first language is present
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

-- Per-item override of the default profile of its kind (movies, series; an
-- episode follows its series). profile_id: a profile, or -1 = no subtitles.
CREATE TABLE subtitle_item_profiles (
    item_id     INTEGER PRIMARY KEY REFERENCES media_items (id) ON DELETE CASCADE,
    profile_id  INTEGER NOT NULL
);
CREATE INDEX subtitle_item_profiles_profile ON subtitle_item_profiles (profile_id);

-- One row per provider implementation; credentials encrypted (D-006).
CREATE TABLE subtitle_providers (
    name             TEXT    PRIMARY KEY,
    enabled          INTEGER NOT NULL DEFAULT 0,
    priority         INTEGER NOT NULL DEFAULT 25,
    username         TEXT    NOT NULL DEFAULT '',
    api_key          BLOB,
    password         BLOB,
    failures         INTEGER NOT NULL DEFAULT 0,
    retry_at         INTEGER,
    last_error       TEXT    NOT NULL DEFAULT '',
    last_error_code  TEXT    NOT NULL DEFAULT '',
    last_error_at    INTEGER,
    last_success_at  INTEGER,
    quota_remaining  INTEGER,
    quota_limit      INTEGER,
    quota_reset_at   INTEGER,
    account          TEXT    NOT NULL DEFAULT '',
    updated_at       INTEGER NOT NULL
);
INSERT INTO subtitle_providers (name, enabled, priority, updated_at) VALUES
    ('opensubtitles', 0, 10, 0), ('subdl', 0, 20, 0), ('jimaku', 0, 30, 0);

-- Subtitle files next to media files: downloaded by Locarr or found on disk.
CREATE TABLE subtitles (
    id               INTEGER PRIMARY KEY,
    media_file_id    INTEGER NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    item_ref         TEXT    NOT NULL,           -- movie:1 or episode:7
    path             TEXT    NOT NULL UNIQUE,
    language         TEXT    NOT NULL,
    forced           INTEGER NOT NULL DEFAULT 0,
    hearing_impaired INTEGER NOT NULL DEFAULT 0,
    format           TEXT    NOT NULL,
    source           TEXT    NOT NULL CHECK (source IN ('downloaded', 'disk')),
    provider         TEXT    NOT NULL DEFAULT '',
    provider_id      TEXT    NOT NULL DEFAULT '',
    release          TEXT    NOT NULL DEFAULT '',
    score            INTEGER,                     -- NULL for files found on disk
    encoding         TEXT    NOT NULL DEFAULT '',
    -- Last re-timing: state '', running, done, failed, not_confident; offset and scale applied.
    sync_state       TEXT    NOT NULL DEFAULT '',
    sync_message     TEXT    NOT NULL DEFAULT '',
    sync_offset_ms   INTEGER NOT NULL DEFAULT 0,
    sync_scale       REAL    NOT NULL DEFAULT 1,
    sync_at          INTEGER,
    added_at         INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
CREATE INDEX subtitles_file ON subtitles (media_file_id);
CREATE INDEX subtitles_item ON subtitles (item_ref);

-- Tracks inside media files (read from the container headers).
CREATE TABLE media_file_tracks (
    media_file_id    INTEGER NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    kind             TEXT    NOT NULL,   -- video, audio, subtitle
    idx              INTEGER NOT NULL,   -- per kind, from 0
    codec            TEXT    NOT NULL DEFAULT '',
    language         TEXT    NOT NULL DEFAULT '',
    name             TEXT    NOT NULL DEFAULT '',
    is_default       INTEGER NOT NULL DEFAULT 0,
    forced           INTEGER NOT NULL DEFAULT 0,
    hearing_impaired INTEGER NOT NULL DEFAULT 0,
    text             INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (media_file_id, kind, idx)
) WITHOUT ROWID;
-- Which media files had their tracks read (and whether that worked).
CREATE TABLE media_file_probes (
    media_file_id    INTEGER PRIMARY KEY REFERENCES media_files (id) ON DELETE CASCADE,
    probed_at        INTEGER NOT NULL,
    error            TEXT    NOT NULL DEFAULT ''
);

-- Every subtitle Locarr downloaded ("never the same subtitle twice").
CREATE TABLE subtitle_history (
    id           INTEGER PRIMARY KEY,
    item_ref     TEXT    NOT NULL,
    provider     TEXT    NOT NULL,
    provider_id  TEXT    NOT NULL,
    language     TEXT    NOT NULL,
    score        INTEGER NOT NULL DEFAULT 0,
    path         TEXT    NOT NULL DEFAULT '',
    at           INTEGER NOT NULL
);
CREATE INDEX subtitle_history_item ON subtitle_history (item_ref, provider, provider_id);
CREATE INDEX subtitle_history_provider_at ON subtitle_history (provider, at);

-- Subtitles the user rejected for an item; never offered automatically again.
CREATE TABLE subtitle_blacklist (
    id           INTEGER PRIMARY KEY,
    item_ref     TEXT    NOT NULL,
    provider     TEXT    NOT NULL,
    provider_id  TEXT    NOT NULL,
    language     TEXT    NOT NULL DEFAULT '',
    release      TEXT    NOT NULL DEFAULT '',
    at           INTEGER NOT NULL,
    UNIQUE (item_ref, provider, provider_id)
);

-- What each item with a file still misses (kept up to date on every change,
-- so the Wanted list is one indexed query) and its search backoff.
CREATE TABLE subtitle_state (
    item_ref         TEXT    PRIMARY KEY,
    item_id          INTEGER NOT NULL,       -- media_items id (movie or series)
    entity_id        INTEGER NOT NULL,       -- the movie's item id or the episode id
    kind             TEXT    NOT NULL,       -- movie, episode
    media_file_id    INTEGER NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    missing          TEXT    NOT NULL DEFAULT '[]',  -- JSON array of wanted keys ("hr", "hr:forced", "en:hi")
    missing_count    INTEGER NOT NULL DEFAULT 0,
    upgradable       INTEGER NOT NULL DEFAULT 0,     -- has a downloaded subtitle below a hash match within the upgrade window
    searches         INTEGER NOT NULL DEFAULT 0,
    last_search_at   INTEGER,
    next_search_at   INTEGER,
    last_result      TEXT    NOT NULL DEFAULT '',
    providers_asked  INTEGER NOT NULL DEFAULT 0,
    file_added_at    INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
CREATE INDEX subtitle_state_missing ON subtitle_state (next_search_at) WHERE missing_count > 0;
CREATE INDEX subtitle_state_wanted ON subtitle_state (file_added_at) WHERE missing_count > 0;
CREATE INDEX subtitle_state_wanted_kind ON subtitle_state (kind, file_added_at) WHERE missing_count > 0;
CREATE INDEX subtitle_state_file ON subtitle_state (media_file_id);

-- +goose Down
DROP TABLE subtitle_state;
DROP TABLE subtitle_blacklist;
DROP TABLE subtitle_history;
DROP TABLE media_file_probes;
DROP TABLE media_file_tracks;
DROP TABLE subtitles;
DROP TABLE subtitle_providers;
DROP TABLE subtitle_item_profiles;
DROP TABLE subtitle_profiles;
