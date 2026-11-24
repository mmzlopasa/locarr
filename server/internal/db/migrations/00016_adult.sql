-- +goose Up
-- Optional adult library (D-102 .. D-105). Everything lives in its own
-- tables, so no query over media_items, movies, series or root_folders can
-- ever return adult rows. The feature is off until an administrator enables
-- it (setting adult.enabled); access is the separate view_adult permission.

CREATE TABLE adult_root_folders (
    id          INTEGER PRIMARY KEY,
    path        TEXT    NOT NULL UNIQUE,
    created_at  INTEGER NOT NULL
);

-- Studios / sites (the "series" of scenes).
CREATE TABLE adult_studios (
    id                  INTEGER PRIMARY KEY,
    name                TEXT    NOT NULL,
    sort_name           TEXT    NOT NULL,
    stashdb_id          TEXT    NOT NULL DEFAULT '',
    tpdb_id             TEXT    NOT NULL DEFAULT '',
    url                 TEXT    NOT NULL DEFAULT '',
    image               TEXT    NOT NULL DEFAULT '',  -- private image cache key
    monitored           INTEGER NOT NULL DEFAULT 1,
    monitor_since       TEXT    NOT NULL DEFAULT '',  -- YYYY-MM-DD: newer scenes become wanted
    quality_profile_id  INTEGER NOT NULL REFERENCES quality_profiles (id),
    root_folder_id      INTEGER REFERENCES adult_root_folders (id) ON DELETE SET NULL,
    path                TEXT    NOT NULL UNIQUE,
    tags                TEXT    NOT NULL DEFAULT '[]',
    added_at            INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    refreshed_at        INTEGER
);
CREATE UNIQUE INDEX adult_studios_stashdb ON adult_studios (stashdb_id) WHERE stashdb_id <> '';
CREATE UNIQUE INDEX adult_studios_tpdb ON adult_studios (tpdb_id) WHERE tpdb_id <> '';
CREATE INDEX adult_studios_sort ON adult_studios (sort_name, id);

CREATE TABLE adult_performers (
    id                  INTEGER PRIMARY KEY,
    name                TEXT    NOT NULL,
    sort_name           TEXT    NOT NULL,
    disambiguation      TEXT    NOT NULL DEFAULT '',
    stashdb_id          TEXT    NOT NULL DEFAULT '',
    tpdb_id             TEXT    NOT NULL DEFAULT '',
    image               TEXT    NOT NULL DEFAULT '',
    monitored           INTEGER NOT NULL DEFAULT 0,  -- monitored: new scenes and movies with them become wanted
    monitor_since       TEXT    NOT NULL DEFAULT '',
    quality_profile_id  INTEGER REFERENCES quality_profiles (id),
    root_folder_id      INTEGER REFERENCES adult_root_folders (id) ON DELETE SET NULL,
    added_at            INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    refreshed_at        INTEGER
);
CREATE UNIQUE INDEX adult_performers_stashdb ON adult_performers (stashdb_id) WHERE stashdb_id <> '';
CREATE UNIQUE INDEX adult_performers_tpdb ON adult_performers (tpdb_id) WHERE tpdb_id <> '';
CREATE INDEX adult_performers_sort ON adult_performers (sort_name, id);

-- Scenes (studio + date + performers) and movies (title + year + studio).
CREATE TABLE adult_items (
    id                    INTEGER PRIMARY KEY,
    kind                  TEXT    NOT NULL CHECK (kind IN ('scene', 'movie')),
    studio_id             INTEGER REFERENCES adult_studios (id) ON DELETE CASCADE,
    studio_name           TEXT    NOT NULL DEFAULT '',
    title                 TEXT    NOT NULL,
    sort_title            TEXT    NOT NULL,
    release_date          TEXT    NOT NULL DEFAULT '',  -- YYYY-MM-DD
    year                  INTEGER NOT NULL DEFAULT 0,
    duration              INTEGER NOT NULL DEFAULT 0,   -- seconds
    overview              TEXT    NOT NULL DEFAULT '',
    stashdb_id            TEXT    NOT NULL DEFAULT '',
    tpdb_id               TEXT    NOT NULL DEFAULT '',
    phash                 TEXT    NOT NULL DEFAULT '',
    source_tags           TEXT    NOT NULL DEFAULT '[]',
    image                 TEXT    NOT NULL DEFAULT '',
    monitored             INTEGER NOT NULL DEFAULT 1,
    quality_profile_id    INTEGER NOT NULL REFERENCES quality_profiles (id),
    root_folder_id        INTEGER REFERENCES adult_root_folders (id) ON DELETE SET NULL,
    path                  TEXT    NOT NULL,   -- folder the file goes to
    tags                  TEXT    NOT NULL DEFAULT '[]',
    added_at              INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);
CREATE UNIQUE INDEX adult_items_stashdb ON adult_items (stashdb_id) WHERE stashdb_id <> '';
CREATE UNIQUE INDEX adult_items_tpdb ON adult_items (tpdb_id) WHERE tpdb_id <> '';
CREATE INDEX adult_items_kind_sort ON adult_items (kind, sort_title, id);
CREATE INDEX adult_items_studio_date ON adult_items (studio_id, release_date);
CREATE INDEX adult_items_date ON adult_items (release_date);
CREATE INDEX adult_items_monitored ON adult_items (monitored, kind);

CREATE TABLE adult_item_performers (
    item_id       INTEGER NOT NULL REFERENCES adult_items (id) ON DELETE CASCADE,
    performer_id  INTEGER NOT NULL REFERENCES adult_performers (id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, performer_id)
);
CREATE INDEX adult_item_performers_performer ON adult_item_performers (performer_id);

-- At most one file per scene or movie.
CREATE TABLE adult_files (
    id              INTEGER PRIMARY KEY,
    item_id         INTEGER NOT NULL UNIQUE REFERENCES adult_items (id) ON DELETE CASCADE,
    path            TEXT    NOT NULL UNIQUE,
    size            INTEGER NOT NULL,
    quality         TEXT    NOT NULL,
    revision        TEXT    NOT NULL DEFAULT '{"version":1}',
    custom_formats  TEXT    NOT NULL DEFAULT '[]',
    score           INTEGER NOT NULL DEFAULT 0,
    release_group   TEXT    NOT NULL DEFAULT '',
    scene_name      TEXT    NOT NULL DEFAULT '',
    download_id     INTEGER REFERENCES downloads (id) ON DELETE SET NULL,
    added_at        INTEGER NOT NULL
);

-- Admin-managed tag blocklist: releases, imports and metadata entries that
-- match a term are rejected (reason blocked_tag).
CREATE TABLE adult_tag_blocklist (
    id        INTEGER PRIMARY KEY,
    term      TEXT    NOT NULL UNIQUE,  -- lower case
    builtin   INTEGER NOT NULL DEFAULT 0,
    added_at  INTEGER NOT NULL
);

-- Read-only keys see adult data only when created with this flag (and the
-- owner has view_adult).
ALTER TABLE api_keys ADD COLUMN include_adult INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN include_adult;
DROP TABLE adult_tag_blocklist;
DROP TABLE adult_files;
DROP TABLE adult_item_performers;
DROP TABLE adult_items;
DROP TABLE adult_performers;
DROP TABLE adult_studios;
DROP TABLE adult_root_folders;
