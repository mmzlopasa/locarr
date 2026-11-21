-- +goose Up
-- Prowlarr / Jackett instances whose indexers are imported (P1).
CREATE TABLE indexer_sources (
    id            INTEGER PRIMARY KEY,
    kind          TEXT    NOT NULL CHECK (kind IN ('prowlarr', 'jackett')),
    name          TEXT    NOT NULL,
    url           TEXT    NOT NULL,
    api_key       BLOB    NOT NULL,           -- encrypted
    exclude       TEXT    NOT NULL DEFAULT '[]', -- JSON array of source refs not to import
    last_sync_at  INTEGER,
    last_error    TEXT,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE indexers (
    id                       INTEGER PRIMARY KEY,
    name                     TEXT    NOT NULL,
    name_key                 TEXT    NOT NULL UNIQUE, -- lower-cased name
    kind                     TEXT    NOT NULL CHECK (kind IN ('torznab', 'newznab')),
    protocol                 TEXT    NOT NULL CHECK (protocol IN ('torrent', 'usenet')),
    base_url                 TEXT    NOT NULL,
    api_path                 TEXT    NOT NULL DEFAULT '/api',
    api_key                  BLOB,               -- encrypted
    enabled                  INTEGER NOT NULL DEFAULT 1,
    enable_rss               INTEGER NOT NULL DEFAULT 1,
    enable_automatic         INTEGER NOT NULL DEFAULT 1,
    enable_interactive       INTEGER NOT NULL DEFAULT 1,
    priority                 INTEGER NOT NULL DEFAULT 25,
    tags                     TEXT    NOT NULL DEFAULT '[]',  -- JSON array
    categories               TEXT    NOT NULL DEFAULT '{}',  -- JSON {movies,tv,music,books}
    minimum_seeders          INTEGER NOT NULL DEFAULT 1,
    seed_ratio               REAL,
    seed_time_minutes        INTEGER,
    request_limit            INTEGER,
    request_period_seconds   INTEGER,
    grab_limit_per_day       INTEGER,
    timeout_seconds          INTEGER NOT NULL DEFAULT 30,
    proxy_url                TEXT,
    proxy_username           TEXT,
    proxy_password           BLOB,               -- encrypted
    flaresolverr_url         TEXT,
    source_id                INTEGER REFERENCES indexer_sources (id) ON DELETE SET NULL,
    source_ref               TEXT,
    caps                     TEXT,               -- JSON capabilities
    caps_updated_at          INTEGER,
    failures                 INTEGER NOT NULL DEFAULT 0,
    retry_at                 INTEGER,            -- skipped until then after failures
    last_error               TEXT,
    last_error_code          TEXT,
    last_error_at            INTEGER,
    last_success_at          INTEGER,
    created_at               INTEGER NOT NULL,
    updated_at               INTEGER NOT NULL
);
CREATE UNIQUE INDEX indexers_source_ref ON indexers (source_id, source_ref) WHERE source_id IS NOT NULL;

-- Grabs per indexer, for the daily grab limit.
CREATE TABLE indexer_grabs (
    id          INTEGER PRIMARY KEY,
    indexer_id  INTEGER NOT NULL REFERENCES indexers (id) ON DELETE CASCADE,
    at          INTEGER NOT NULL
);
CREATE INDEX indexer_grabs_indexer_at ON indexer_grabs (indexer_id, at);

-- Every search (interactive, automatic, RSS, test). Later phases build the
-- per-title timeline on it through item_ref (e.g. "movie:42", "episode:7").
CREATE TABLE search_log (
    id            INTEGER PRIMARY KEY,
    started_at    INTEGER NOT NULL,
    duration_ms   INTEGER NOT NULL,
    source        TEXT    NOT NULL CHECK (source IN ('interactive', 'automatic', 'rss', 'test')),
    search_type   TEXT    NOT NULL,
    query         TEXT    NOT NULL,   -- human-readable summary
    params        TEXT    NOT NULL,   -- JSON of the search (no secrets)
    item_ref      TEXT,
    user_id       INTEGER REFERENCES users (id) ON DELETE SET NULL,
    result_count  INTEGER NOT NULL,
    error_count   INTEGER NOT NULL
);
CREATE INDEX search_log_started ON search_log (started_at);
CREATE INDEX search_log_item ON search_log (item_ref, started_at) WHERE item_ref IS NOT NULL;

CREATE TABLE search_log_indexers (
    search_id     INTEGER NOT NULL REFERENCES search_log (id) ON DELETE CASCADE,
    indexer_id    INTEGER,            -- kept when the indexer is deleted
    indexer_name  TEXT    NOT NULL,
    status        TEXT    NOT NULL,
    result_count  INTEGER NOT NULL,
    cached        INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL,
    error_code    TEXT,
    error         TEXT,
    PRIMARY KEY (search_id, indexer_name)
);

-- Releases seen in RSS feeds (deduplicated per indexer, pruned after the retention).
CREATE TABLE releases_seen (
    id             INTEGER PRIMARY KEY,
    indexer_id     INTEGER NOT NULL REFERENCES indexers (id) ON DELETE CASCADE,
    guid           TEXT    NOT NULL,
    info_hash      TEXT,
    title          TEXT    NOT NULL,
    size           INTEGER,
    publish_date   INTEGER,
    first_seen_at  INTEGER NOT NULL,
    data           TEXT    NOT NULL,  -- JSON of the release (API keys stripped)
    UNIQUE (indexer_id, guid)
);
CREATE INDEX releases_seen_first_seen ON releases_seen (first_seen_at);
CREATE INDEX releases_seen_hash ON releases_seen (info_hash) WHERE info_hash IS NOT NULL;

-- +goose Down
DROP TABLE releases_seen;
DROP TABLE search_log_indexers;
DROP TABLE search_log;
DROP TABLE indexer_grabs;
DROP TABLE indexers;
DROP TABLE indexer_sources;
