-- +goose Up
-- P5: TV series on the shared library core. A series is a media_items row
-- (kind 'series') with its facts in `series`; seasons and episodes have their
-- own tables. Episode files live in media_files (item_id = the series) and
-- episodes point at them, so one multi-episode file serves several episodes.

CREATE TABLE series (
    item_id               INTEGER PRIMARY KEY REFERENCES media_items (id) ON DELETE CASCADE,
    tmdb_id               INTEGER NOT NULL DEFAULT 0,
    tvdb_id               INTEGER NOT NULL DEFAULT 0,
    imdb_id               TEXT    NOT NULL DEFAULT '',
    tvmaze_id             INTEGER NOT NULL DEFAULT 0,
    metadata_source       TEXT    NOT NULL DEFAULT 'tmdb' CHECK (metadata_source IN ('tmdb', 'tvdb')),
    original_title        TEXT    NOT NULL DEFAULT '',
    original_language     TEXT    NOT NULL DEFAULT '',
    alternate_titles      TEXT    NOT NULL DEFAULT '[]',
    overview              TEXT    NOT NULL DEFAULT '',
    status                TEXT    NOT NULL DEFAULT 'continuing' CHECK (status IN ('continuing', 'ended', 'upcoming')),
    network               TEXT    NOT NULL DEFAULT '',
    time_zone             TEXT    NOT NULL DEFAULT '',  -- IANA zone of the network
    air_time              TEXT    NOT NULL DEFAULT '',  -- HH:MM local ('' = unknown)
    runtime               INTEGER NOT NULL DEFAULT 0,   -- minutes per episode
    genres                TEXT    NOT NULL DEFAULT '[]',
    certification         TEXT    NOT NULL DEFAULT '',
    rating                REAL    NOT NULL DEFAULT 0,
    votes                 INTEGER NOT NULL DEFAULT 0,
    popularity            REAL    NOT NULL DEFAULT 0,
    first_aired           TEXT    NOT NULL DEFAULT '',  -- YYYY-MM-DD
    last_aired            TEXT    NOT NULL DEFAULT '',
    series_type           TEXT    NOT NULL DEFAULT 'standard' CHECK (series_type IN ('standard', 'daily', 'anime')),
    season_folder         INTEGER NOT NULL DEFAULT 1,
    monitor_new_seasons   TEXT    NOT NULL DEFAULT 'all' CHECK (monitor_new_seasons IN ('all', 'none')),
    poster                TEXT    NOT NULL DEFAULT '',
    backdrop              TEXT    NOT NULL DEFAULT '',
    metadata_refreshed_at INTEGER
);
CREATE UNIQUE INDEX series_tmdb ON series (tmdb_id) WHERE tmdb_id <> 0;
CREATE UNIQUE INDEX series_tvdb ON series (tvdb_id) WHERE tvdb_id <> 0;
CREATE INDEX series_imdb ON series (imdb_id) WHERE imdb_id <> '';
CREATE INDEX series_status ON series (status);

CREATE TABLE seasons (
    id          INTEGER PRIMARY KEY,
    series_id   INTEGER NOT NULL REFERENCES media_items (id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    monitored   INTEGER NOT NULL DEFAULT 1,
    title       TEXT    NOT NULL DEFAULT '',
    overview    TEXT    NOT NULL DEFAULT '',
    poster      TEXT    NOT NULL DEFAULT '',
    air_date    TEXT    NOT NULL DEFAULT '',
    UNIQUE (series_id, number)
);

CREATE TABLE episodes (
    id              INTEGER PRIMARY KEY,
    series_id       INTEGER NOT NULL REFERENCES media_items (id) ON DELETE CASCADE,
    season_id       INTEGER NOT NULL REFERENCES seasons (id) ON DELETE CASCADE,
    season_number   INTEGER NOT NULL,
    episode_number  INTEGER NOT NULL,
    absolute_number INTEGER NOT NULL DEFAULT 0,
    tmdb_id         INTEGER NOT NULL DEFAULT 0,
    tvdb_id         INTEGER NOT NULL DEFAULT 0,
    title           TEXT    NOT NULL DEFAULT '',
    overview        TEXT    NOT NULL DEFAULT '',
    air_date        TEXT    NOT NULL DEFAULT '',   -- YYYY-MM-DD in the network's time zone
    air_date_utc    INTEGER,                       -- millis; when it airs (D-039)
    runtime         INTEGER NOT NULL DEFAULT 0,
    monitored       INTEGER NOT NULL DEFAULT 1,
    file_id         INTEGER REFERENCES media_files (id) ON DELETE SET NULL,
    -- Manual scene mapping (D-040): how releases number this episode.
    scene_season    INTEGER,
    scene_episode   INTEGER,
    scene_absolute  INTEGER,
    -- The provider no longer lists it; kept because it has a file.
    removed         INTEGER NOT NULL DEFAULT 0,
    added_at        INTEGER NOT NULL,
    UNIQUE (series_id, season_number, episode_number)
);
CREATE INDEX episodes_season ON episodes (season_id);
CREATE INDEX episodes_air_date ON episodes (air_date) WHERE air_date <> '';
CREATE INDEX episodes_absolute ON episodes (series_id, absolute_number) WHERE absolute_number > 0;
CREATE INDEX episodes_file ON episodes (file_id) WHERE file_id IS NOT NULL;
-- Missing episodes (the Wanted list and the "missing" filter) without touching the table.
CREATE INDEX episodes_missing ON episodes (series_id, air_date_utc, season_id) WHERE file_id IS NULL AND monitored = 1 AND removed = 0;

-- Every item a download is for (its ref and what it covers), filled by
-- triggers from downloads.item_ref so lookups by item use an index.
CREATE TABLE download_refs (
    item_ref     TEXT    NOT NULL,
    download_id  INTEGER NOT NULL REFERENCES downloads (id) ON DELETE CASCADE,
    PRIMARY KEY (item_ref, download_id)
) WITHOUT ROWID;
CREATE INDEX download_refs_download ON download_refs (download_id);

INSERT OR IGNORE INTO download_refs (item_ref, download_id)
    SELECT json_extract(item_ref, '$.ref'), id FROM downloads
    WHERE json_valid(item_ref) AND json_extract(item_ref, '$.ref') IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER downloads_refs_insert AFTER INSERT ON downloads WHEN json_valid(NEW.item_ref)
BEGIN
    INSERT OR IGNORE INTO download_refs (item_ref, download_id)
        SELECT json_extract(NEW.item_ref, '$.ref'), NEW.id WHERE json_extract(NEW.item_ref, '$.ref') IS NOT NULL;
    INSERT OR IGNORE INTO download_refs (item_ref, download_id)
        SELECT value, NEW.id FROM json_each(NEW.item_ref, '$.covers') WHERE type = 'text';
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER downloads_refs_update AFTER UPDATE OF item_ref ON downloads
BEGIN
    DELETE FROM download_refs WHERE download_id = NEW.id;
    INSERT OR IGNORE INTO download_refs (item_ref, download_id)
        SELECT json_extract(NEW.item_ref, '$.ref'), NEW.id WHERE json_valid(NEW.item_ref) AND json_extract(NEW.item_ref, '$.ref') IS NOT NULL;
    INSERT OR IGNORE INTO download_refs (item_ref, download_id)
        SELECT value, NEW.id FROM json_each(CASE WHEN json_valid(NEW.item_ref) THEN NEW.item_ref ELSE '{}' END, '$.covers') WHERE type = 'text';
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER downloads_refs_update;
DROP TRIGGER downloads_refs_insert;
DROP TABLE download_refs;
DROP TABLE episodes;
DROP TABLE seasons;
DROP TABLE series;
