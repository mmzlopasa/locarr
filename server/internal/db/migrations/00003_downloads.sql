-- +goose Up
-- Download clients (P3). Passwords and API keys are AES-GCM encrypted (D-006).
CREATE TABLE download_clients (
    id                 INTEGER PRIMARY KEY,
    name               TEXT    NOT NULL UNIQUE,
    implementation     TEXT    NOT NULL,
    protocol           TEXT    NOT NULL CHECK (protocol IN ('torrent', 'usenet')),
    enabled            INTEGER NOT NULL DEFAULT 1,
    priority           INTEGER NOT NULL DEFAULT 1,  -- lower is tried first among clients of one protocol
    host               TEXT    NOT NULL,
    port               INTEGER NOT NULL,
    use_tls            INTEGER NOT NULL DEFAULT 0,
    allow_self_signed  INTEGER NOT NULL DEFAULT 0,
    url_base           TEXT    NOT NULL DEFAULT '',
    username           TEXT    NOT NULL DEFAULT '',
    password           BLOB,
    api_key            BLOB,
    directory          TEXT    NOT NULL DEFAULT '',
    category_movies    TEXT    NOT NULL DEFAULT '',
    category_series    TEXT    NOT NULL DEFAULT '',
    category_music     TEXT    NOT NULL DEFAULT '',
    recent_priority    TEXT    NOT NULL DEFAULT 'normal' CHECK (recent_priority IN ('normal', 'top', 'last')),
    older_priority     TEXT    NOT NULL DEFAULT 'normal' CHECK (older_priority IN ('normal', 'top', 'last')),
    initial_state      TEXT    NOT NULL DEFAULT 'start' CHECK (initial_state IN ('start', 'pause')),
    remove_completed   INTEGER NOT NULL DEFAULT 0,
    sequential         INTEGER NOT NULL DEFAULT 0,
    first_last_piece   INTEGER NOT NULL DEFAULT 0,
    tags               TEXT    NOT NULL DEFAULT '[]', -- JSON array: used only for releases with a matching tag (none = all)
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
);

-- A client on another machine or container sees the files under another path.
CREATE TABLE remote_path_mappings (
    id           INTEGER PRIMARY KEY,
    host         TEXT    NOT NULL,
    remote_path  TEXT    NOT NULL,
    local_path   TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE (host, remote_path)
);

-- One row per release sent to a client. item_ref is opaque JSON owned by the
-- importers (movies, series, music); tracking never interprets it.
CREATE TABLE downloads (
    id               INTEGER PRIMARY KEY,
    client_id        INTEGER REFERENCES download_clients (id) ON DELETE SET NULL,
    client_name      TEXT    NOT NULL,
    client_item_id   TEXT    NOT NULL,
    protocol         TEXT    NOT NULL CHECK (protocol IN ('torrent', 'usenet')),
    title            TEXT    NOT NULL,
    indexer_id       INTEGER,
    indexer          TEXT    NOT NULL DEFAULT '',
    guid             TEXT    NOT NULL DEFAULT '',
    info_hash        TEXT    NOT NULL DEFAULT '',
    category         TEXT    NOT NULL DEFAULT '',
    media_kind       TEXT    NOT NULL DEFAULT '',
    item_ref         TEXT    NOT NULL DEFAULT '{}',
    seed_ratio       REAL,
    seed_time        INTEGER,                 -- seconds
    status           TEXT    NOT NULL CHECK (status IN ('active', 'completed', 'imported', 'failed', 'removed')),
    state            TEXT    NOT NULL,        -- normalised client state
    raw_state        TEXT    NOT NULL DEFAULT '',
    progress         REAL    NOT NULL DEFAULT 0,
    size             INTEGER NOT NULL DEFAULT 0,
    downloaded       INTEGER NOT NULL DEFAULT 0,
    download_speed   INTEGER NOT NULL DEFAULT 0,
    upload_speed     INTEGER NOT NULL DEFAULT 0,
    eta              INTEGER,                 -- seconds
    ratio            REAL    NOT NULL DEFAULT 0,
    seeding_time     INTEGER NOT NULL DEFAULT 0,
    save_path        TEXT    NOT NULL DEFAULT '',
    content_path     TEXT    NOT NULL DEFAULT '',
    local_path       TEXT    NOT NULL DEFAULT '',
    message          TEXT    NOT NULL DEFAULT '',
    stalled_since    INTEGER,
    missing_count    INTEGER NOT NULL DEFAULT 0,
    added_at         INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    completed_at     INTEGER,
    finished_at      INTEGER                  -- imported, failed or removed
);
CREATE UNIQUE INDEX downloads_client_item ON downloads (client_id, client_item_id) WHERE status IN ('active', 'completed', 'imported');
CREATE INDEX downloads_status ON downloads (status, client_id);
CREATE INDEX downloads_added ON downloads (added_at);

-- Every state change of a download; the per-title timeline reads it, so the
-- columns are stable: type is one of grabbed, state, completed, failed,
-- removed, imported, import_failed, blocklisted.
CREATE TABLE download_events (
    id           INTEGER PRIMARY KEY,
    download_id  INTEGER NOT NULL REFERENCES downloads (id) ON DELETE CASCADE,
    at           INTEGER NOT NULL,
    type         TEXT    NOT NULL,
    from_state   TEXT    NOT NULL DEFAULT '',
    to_state     TEXT    NOT NULL DEFAULT '',
    message      TEXT    NOT NULL DEFAULT '',
    data         TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX download_events_download ON download_events (download_id, at);
CREATE INDEX download_events_at ON download_events (at);

CREATE TABLE blocklist (
    id          INTEGER PRIMARY KEY,
    title       TEXT    NOT NULL,
    protocol    TEXT    NOT NULL DEFAULT '',
    indexer_id  INTEGER,
    indexer     TEXT    NOT NULL DEFAULT '',
    info_hash   TEXT    NOT NULL DEFAULT '',
    guid        TEXT    NOT NULL DEFAULT '',
    reason      TEXT    NOT NULL,
    message     TEXT    NOT NULL DEFAULT '',
    item_ref    TEXT    NOT NULL DEFAULT '{}',
    download_id INTEGER REFERENCES downloads (id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL
);
CREATE INDEX blocklist_info_hash ON blocklist (info_hash) WHERE info_hash <> '';
CREATE INDEX blocklist_guid ON blocklist (guid) WHERE guid <> '';
CREATE INDEX blocklist_title ON blocklist (title COLLATE NOCASE);
CREATE INDEX blocklist_created ON blocklist (created_at);

-- +goose Down
DROP TABLE blocklist;
DROP TABLE download_events;
DROP TABLE downloads;
DROP TABLE remote_path_mappings;
DROP TABLE download_clients;
