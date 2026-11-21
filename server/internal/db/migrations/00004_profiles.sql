-- +goose Up
-- Size limits per quality and media kind (MB per minute of runtime).
CREATE TABLE quality_definitions (
    kind        TEXT    NOT NULL CHECK (kind IN ('movies', 'series', 'music')),
    quality     TEXT    NOT NULL,
    min_mb      REAL    NOT NULL DEFAULT 0,
    preferred_mb REAL   NOT NULL DEFAULT 0,
    max_mb      REAL    NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (kind, quality)
);

-- Profiles keep their structured parts (items, scores, languages) as JSON.
CREATE TABLE quality_profiles (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('movies', 'series', 'music')),
    data        TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    UNIQUE (kind, name)
);

CREATE TABLE custom_formats (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    data        TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE release_profiles (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    data        TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE delay_profiles (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    sort_order  INTEGER NOT NULL,
    is_default  INTEGER NOT NULL DEFAULT 0,
    data        TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX delay_profiles_default ON delay_profiles (is_default) WHERE is_default = 1;

-- +goose Down
DROP TABLE delay_profiles;
DROP TABLE release_profiles;
DROP TABLE custom_formats;
DROP TABLE quality_profiles;
DROP TABLE quality_definitions;
