-- +goose Up
-- P11: timeline, monitoring and operations (D-081 … D-086).

-- "Why didn't it land?" board: one row per monitored item that is missing
-- (movie, episode, album) or misses subtitles, with the explainer's current
-- answer. Rebuilt in the background and refreshed per item on events, so the
-- board is a couple of indexed queries even with tens of thousands of items.
CREATE TABLE board_items (
    key          TEXT    PRIMARY KEY,   -- "movie:1", "episode:7", "album:3", "subtitle:movie:1"
    kind         TEXT    NOT NULL CHECK (kind IN ('movie', 'episode', 'album', 'subtitle')),
    item_ref     TEXT    NOT NULL,      -- the library item ("movie:1"; for subtitles the video)
    parent_ref   TEXT    NOT NULL DEFAULT '', -- series or artist the item belongs to
    title        TEXT    NOT NULL,
    sort_title   TEXT    NOT NULL,
    code         TEXT    NOT NULL,      -- explanation code
    tone         TEXT    NOT NULL,
    fix          TEXT    NOT NULL DEFAULT '', -- what to fix: path_mapping, permissions, client_category, ...
    message      TEXT    NOT NULL DEFAULT '',
    params       TEXT    NOT NULL DEFAULT '{}',
    reasons      TEXT    NOT NULL DEFAULT '[]', -- top rejection reasons of the last search
    searches     INTEGER NOT NULL DEFAULT 0,
    download_id  INTEGER,
    quality_profile_id INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX board_items_code ON board_items (code, kind, sort_title);
CREATE INDEX board_items_kind ON board_items (kind, sort_title);
CREATE INDEX board_items_ref ON board_items (item_ref);
CREATE INDEX board_items_parent ON board_items (parent_ref) WHERE parent_ref <> '';

-- The board reads the latest decision of many items at once.
CREATE INDEX timeline_events_type_ref ON timeline_events (type, item_ref, at);
-- History filters by type and time.
CREATE INDEX timeline_events_type_at ON timeline_events (type, at);
CREATE INDEX download_events_type_at ON download_events (type, at);
-- Statistics per indexer and client.
CREATE INDEX downloads_indexer ON downloads (indexer_id, added_at);
CREATE INDEX downloads_client ON downloads (client_id, added_at);
CREATE INDEX downloads_finished ON downloads (finished_at) WHERE finished_at IS NOT NULL;

-- +goose Down
DROP INDEX downloads_finished;
DROP INDEX downloads_client;
DROP INDEX downloads_indexer;
DROP INDEX download_events_type_at;
DROP INDEX timeline_events_type_at;
DROP INDEX timeline_events_type_ref;
DROP TABLE board_items;
