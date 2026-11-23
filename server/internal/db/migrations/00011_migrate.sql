-- +goose Up
-- P10: importing an existing setup from other applications through their
-- public APIs (D-003, D-072 … D-078). Nothing here is ever sent back to them.

-- An application Locarr reads from. The API key (or the password for
-- qBittorrent) is sealed with the secret box and never returned.
CREATE TABLE migrate_sources (
    id            INTEGER PRIMARY KEY,
    kind          TEXT    NOT NULL CHECK (kind IN ('sonarr', 'radarr', 'lidarr', 'prowlarr', 'bazarr', 'overseerr', 'seerr',
                                                   'qbittorrent', 'sabnzbd')),
    name          TEXT    NOT NULL,
    url           TEXT    NOT NULL,
    username      TEXT    NOT NULL DEFAULT '',
    secret        BLOB,
    app_name      TEXT    NOT NULL DEFAULT '',
    version       TEXT    NOT NULL DEFAULT '',
    last_test_at  INTEGER,
    last_error    TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE (kind, url)
);

-- One pass of the wizard: read (analyse), plan (preview) and import (apply).
CREATE TABLE migrate_runs (
    id           INTEGER PRIMARY KEY,
    status       TEXT    NOT NULL CHECK (status IN ('analysing', 'analysed', 'planned', 'importing', 'done', 'failed', 'cancelled')),
    options      TEXT    NOT NULL DEFAULT '{}',   -- what to read from which source, path mappings, conflict choices
    progress     TEXT    NOT NULL DEFAULT '{}',   -- {phase, done, total, message}
    summary      TEXT    NOT NULL DEFAULT '{}',   -- counts, warnings, path checks, attention
    error        TEXT    NOT NULL DEFAULT '',
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    finished_at  INTEGER
);
CREATE INDEX migrate_runs_created ON migrate_runs (created_at);

-- The plan of a run, one row per thing that would be (or was) created,
-- merged or skipped, with the reason.
CREATE TABLE migrate_items (
    run_id     INTEGER NOT NULL REFERENCES migrate_runs (id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    key        TEXT    NOT NULL,
    source_id  INTEGER NOT NULL,
    category   TEXT    NOT NULL,
    name       TEXT    NOT NULL,
    action     TEXT    NOT NULL CHECK (action IN ('create', 'update', 'skip')),
    reason     TEXT    NOT NULL DEFAULT '',
    message    TEXT    NOT NULL DEFAULT '',
    conflict   TEXT    NOT NULL DEFAULT '',   -- JSON {existing, options, chosen} or ''
    attention  TEXT    NOT NULL DEFAULT '[]', -- JSON list of codes
    status     TEXT    NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'done', 'failed', 'skipped')),
    local_ref  TEXT    NOT NULL DEFAULT '',
    error      TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, seq)
);
CREATE INDEX migrate_items_category ON migrate_items (run_id, category, action);

-- What was imported from where: re-running updates these instead of adding
-- duplicates, and items keep a mark of their origin. source_key is
-- "kind|url" so it survives removing and adding a source again.
CREATE TABLE migrate_links (
    source_key   TEXT    NOT NULL,
    entity       TEXT    NOT NULL,
    external_id  TEXT    NOT NULL,
    local_ref    TEXT    NOT NULL,
    hash         TEXT    NOT NULL DEFAULT '',
    run_id       INTEGER,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (source_key, entity, external_id)
);
CREATE INDEX migrate_links_local ON migrate_links (local_ref);

-- +goose Down
DROP TABLE migrate_links;
DROP TABLE migrate_items;
DROP TABLE migrate_runs;
DROP TABLE migrate_sources;
