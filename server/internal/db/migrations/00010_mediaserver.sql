-- +goose Up
-- P9: Streamero (library updates, titles it already has) and outward
-- notification channels (D-067 … D-071).

-- Library changes waiting to be sent to Streamero as library scans. Rows are
-- removed once the scan was asked for (or finally failed).
CREATE TABLE mediaserver_pending (
    id          INTEGER PRIMARY KEY,
    item_ref    TEXT    NOT NULL,
    action      TEXT    NOT NULL,
    title       TEXT    NOT NULL DEFAULT '',
    paths       TEXT    NOT NULL DEFAULT '[]', -- JSON array of Locarr paths
    created_at  INTEGER NOT NULL
);

-- Titles Streamero already has (movies and series), refreshed from its API.
-- id is Streamero's item id.
CREATE TABLE streamero_items (
    id          TEXT    PRIMARY KEY,
    library_id  TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('movie', 'series')),
    title       TEXT    NOT NULL DEFAULT '',
    year        INTEGER NOT NULL DEFAULT 0,
    tmdb_id     TEXT    NOT NULL DEFAULT '',
    imdb_id     TEXT    NOT NULL DEFAULT '',
    tvdb_id     TEXT    NOT NULL DEFAULT '',
    checked_at  INTEGER NOT NULL
);
CREATE INDEX streamero_items_tmdb ON streamero_items (kind, tmdb_id) WHERE tmdb_id <> '';
CREATE INDEX streamero_items_imdb ON streamero_items (imdb_id) WHERE imdb_id <> '';
CREATE INDEX streamero_items_tvdb ON streamero_items (tvdb_id) WHERE tvdb_id <> '';

-- Notification channels. user_id NULL = shared (administrators choose the
-- events); otherwise a user's own channel for their requests and issues.
-- settings is JSON (plain), secrets is the encrypted JSON of write-only values.
CREATE TABLE notification_channels (
    id             INTEGER PRIMARY KEY,
    user_id        INTEGER REFERENCES users (id) ON DELETE CASCADE,
    name           TEXT    NOT NULL,
    kind           TEXT    NOT NULL,
    enabled        INTEGER NOT NULL DEFAULT 1,
    events         TEXT    NOT NULL DEFAULT '[]',
    settings       TEXT    NOT NULL DEFAULT '{}',
    secrets        BLOB,
    failures       INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT    NOT NULL DEFAULT '',
    last_error_at  INTEGER,
    last_sent_at   INTEGER,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX notification_channels_user ON notification_channels (user_id);

-- Files whose still-missing subtitles were already announced (once per file).
CREATE TABLE subtitle_missing_notified (
    media_file_id  INTEGER PRIMARY KEY REFERENCES media_files (id) ON DELETE CASCADE,
    notified_at    INTEGER NOT NULL
);

-- +goose Down
DROP TABLE subtitle_missing_notified;
DROP TABLE notification_channels;
DROP TABLE streamero_items;
DROP TABLE mediaserver_pending;
