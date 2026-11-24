-- +goose NO TRANSACTION
-- +goose Up
-- History keeps the title an item had (D-107), and library ids are never
-- reused. Before this, media_items, seasons, episodes, albums and
-- adult_items used plain INTEGER PRIMARY KEY: SQLite hands out max(id) + 1,
-- so deleting the newest movie and adding another gave the new one the same
-- id, and every event, search, download and blocklist entry stored as
-- "movie:<id>" suddenly belonged to it.
--
-- The tables are rebuilt with AUTOINCREMENT (SQLite's documented rebuild:
-- foreign keys off, one transaction, checked before commit), so this runs
-- outside goose's transaction.
PRAGMA foreign_keys = OFF;

BEGIN IMMEDIATE;

-- Every item that exists now: its ref, display title and when it was added.
CREATE TEMP TABLE h17_items (ref TEXT PRIMARY KEY, title TEXT NOT NULL, added_at INTEGER NOT NULL);
INSERT OR IGNORE INTO h17_items (ref, title, added_at)
SELECT 'movie:' || id, title || CASE WHEN year > 0 THEN ' (' || year || ')' ELSE '' END, added_at FROM media_items WHERE kind = 'movie'
UNION ALL SELECT 'series:' || id, title, added_at FROM media_items WHERE kind = 'series'
UNION ALL SELECT 'artist:' || id, title, added_at FROM media_items WHERE kind = 'artist'
UNION ALL SELECT 'season:' || sn.id, i.title || ' season ' || sn.number, i.added_at FROM seasons sn JOIN media_items i ON i.id = sn.series_id
UNION ALL SELECT 'episode:' || e.id, i.title || printf(' S%02dE%02d', e.season_number, e.episode_number), e.added_at
    FROM episodes e JOIN media_items i ON i.id = e.series_id
UNION ALL SELECT 'album:' || a.id, i.title || ' - ' || a.title, a.added_at FROM albums a JOIN media_items i ON i.id = a.artist_id
UNION ALL SELECT CASE kind WHEN 'scene' THEN 'scene:' ELSE 'adult_movie:' END || id,
    title || CASE WHEN year > 0 THEN ' (' || year || ')' ELSE '' END, added_at FROM adult_items;

-- Title snapshots.
ALTER TABLE timeline_events ADD COLUMN item_title TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN item_title TEXT NOT NULL DEFAULT '';

-- Rows written before the item now holding their id was added belong to an
-- earlier, deleted item: detach them ("movie:42#removed") so they leave the
-- new item's timeline, explanation, queue and blocklist. History brought in
-- from another application keeps its original dates and stays.
UPDATE timeline_events SET item_ref = item_ref || '#removed'
WHERE type <> 'history' AND at < (SELECT r.added_at FROM h17_items r WHERE r.ref = timeline_events.item_ref);
UPDATE search_log SET item_ref = item_ref || '#removed'
WHERE item_ref IS NOT NULL AND started_at < (SELECT r.added_at FROM h17_items r WHERE r.ref = search_log.item_ref);
UPDATE subtitle_history SET item_ref = item_ref || '#removed'
WHERE at < (SELECT r.added_at FROM h17_items r WHERE r.ref = subtitle_history.item_ref);
UPDATE downloads SET item_ref = json_set(item_ref, '$.ref', json_extract(item_ref, '$.ref') || '#removed', '$.covers', json('[]'))
WHERE json_valid(item_ref) AND added_at < (SELECT r.added_at FROM h17_items r WHERE r.ref = json_extract(downloads.item_ref, '$.ref'));
UPDATE blocklist SET item_ref = json_set(item_ref, '$.ref', json_extract(item_ref, '$.ref') || '#removed', '$.covers', json('[]'))
WHERE json_valid(item_ref) AND created_at < (SELECT r.added_at FROM h17_items r WHERE r.ref = json_extract(blocklist.item_ref, '$.ref'));
-- Requests know what they asked for: a ref pointing at another title is detached.
UPDATE requests SET item_ref = item_ref || '#removed'
WHERE kind = 'movie' AND tmdb_id <> 0 AND item_ref LIKE 'movie:%' AND instr(item_ref, '#') = 0
  AND EXISTS (SELECT 1 FROM movies m WHERE m.item_id = CAST(substr(requests.item_ref, 7) AS INTEGER) AND m.tmdb_id <> requests.tmdb_id);
UPDATE requests SET item_ref = item_ref || '#removed'
WHERE kind = 'series' AND tmdb_id <> 0 AND item_ref LIKE 'series:%' AND instr(item_ref, '#') = 0
  AND EXISTS (SELECT 1 FROM series s WHERE s.item_id = CAST(substr(requests.item_ref, 8) AS INTEGER) AND s.tmdb_id <> 0
      AND s.tmdb_id <> requests.tmdb_id);
UPDATE requests SET item_ref = item_ref || '#removed'
WHERE kind = 'album' AND mbid <> '' AND item_ref LIKE 'album:%' AND instr(item_ref, '#') = 0
  AND EXISTS (SELECT 1 FROM albums a WHERE a.id = CAST(substr(requests.item_ref, 7) AS INTEGER) AND a.mbid <> requests.mbid);

UPDATE timeline_events SET item_title = COALESCE((SELECT r.title FROM h17_items r WHERE r.ref = timeline_events.item_ref), '');
UPDATE downloads SET item_title = COALESCE((SELECT r.title FROM h17_items r WHERE r.ref = json_extract(downloads.item_ref, '$.ref')), '')
WHERE json_valid(item_ref);

-- The highest id any ref ever used, per table, so a new item never takes the
-- id of a deleted one that still has history.
CREATE TEMP TABLE h17_refs (kind TEXT NOT NULL, id INTEGER NOT NULL);
INSERT INTO h17_refs (kind, id)
SELECT substr(ref, 1, instr(ref, ':') - 1), CAST(substr(ref, instr(ref, ':') + 1) AS INTEGER) FROM (
    SELECT item_ref AS ref FROM timeline_events
    UNION SELECT item_ref FROM search_log WHERE item_ref IS NOT NULL
    UNION SELECT item_ref FROM download_refs
    UNION SELECT item_ref FROM subtitle_history
    UNION SELECT item_ref FROM requests WHERE item_ref <> ''
    UNION SELECT json_extract(item_ref, '$.ref') FROM blocklist WHERE json_valid(item_ref) AND json_extract(item_ref, '$.ref') IS NOT NULL
) WHERE instr(ref, ':') > 0;

-- media_items
CREATE TABLE media_items_new (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
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
INSERT INTO media_items_new (id, kind, title, sort_title, year, monitored, quality_profile_id, root_folder_id, path, tags, added_at, updated_at)
SELECT id, kind, title, sort_title, year, monitored, quality_profile_id, root_folder_id, path, tags, added_at, updated_at FROM media_items;
DROP TABLE media_items;
ALTER TABLE media_items_new RENAME TO media_items;
CREATE INDEX media_items_kind_sort ON media_items (kind, sort_title, id);
CREATE INDEX media_items_kind_added ON media_items (kind, added_at);
CREATE INDEX media_items_profile ON media_items (quality_profile_id);

-- seasons
CREATE TABLE seasons_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id   INTEGER NOT NULL REFERENCES media_items (id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    monitored   INTEGER NOT NULL DEFAULT 1,
    title       TEXT    NOT NULL DEFAULT '',
    overview    TEXT    NOT NULL DEFAULT '',
    poster      TEXT    NOT NULL DEFAULT '',
    air_date    TEXT    NOT NULL DEFAULT '',
    UNIQUE (series_id, number)
);
INSERT INTO seasons_new (id, series_id, number, monitored, title, overview, poster, air_date)
SELECT id, series_id, number, monitored, title, overview, poster, air_date FROM seasons;
DROP TABLE seasons;
ALTER TABLE seasons_new RENAME TO seasons;

-- episodes
CREATE TABLE episodes_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
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
    air_date_utc    INTEGER,                       -- millis; when it airs (D-046)
    runtime         INTEGER NOT NULL DEFAULT 0,
    monitored       INTEGER NOT NULL DEFAULT 1,
    file_id         INTEGER REFERENCES media_files (id) ON DELETE SET NULL,
    -- Manual scene mapping (D-047): how releases number this episode.
    scene_season    INTEGER,
    scene_episode   INTEGER,
    scene_absolute  INTEGER,
    -- The provider no longer lists it; kept because it has a file.
    removed         INTEGER NOT NULL DEFAULT 0,
    added_at        INTEGER NOT NULL,
    UNIQUE (series_id, season_number, episode_number)
);
INSERT INTO episodes_new (id, series_id, season_id, season_number, episode_number, absolute_number, tmdb_id, tvdb_id, title, overview,
    air_date, air_date_utc, runtime, monitored, file_id, scene_season, scene_episode, scene_absolute, removed, added_at)
SELECT id, series_id, season_id, season_number, episode_number, absolute_number, tmdb_id, tvdb_id, title, overview,
    air_date, air_date_utc, runtime, monitored, file_id, scene_season, scene_episode, scene_absolute, removed, added_at FROM episodes;
DROP TABLE episodes;
ALTER TABLE episodes_new RENAME TO episodes;
CREATE INDEX episodes_season ON episodes (season_id);
CREATE INDEX episodes_air_date ON episodes (air_date) WHERE air_date <> '';
CREATE INDEX episodes_absolute ON episodes (series_id, absolute_number) WHERE absolute_number > 0;
CREATE INDEX episodes_file ON episodes (file_id) WHERE file_id IS NOT NULL;
CREATE INDEX episodes_missing ON episodes (series_id, air_date_utc, season_id) WHERE file_id IS NULL AND monitored = 1 AND removed = 0;

-- albums
CREATE TABLE albums_new (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    artist_id            INTEGER NOT NULL REFERENCES media_items (id) ON DELETE CASCADE,
    mbid                 TEXT    NOT NULL UNIQUE,
    title                TEXT    NOT NULL,
    sort_title           TEXT    NOT NULL,
    disambiguation       TEXT    NOT NULL DEFAULT '',
    primary_type         TEXT    NOT NULL DEFAULT '',
    secondary_types      TEXT    NOT NULL DEFAULT '[]',
    release_date         TEXT    NOT NULL DEFAULT '',   -- YYYY, YYYY-MM or YYYY-MM-DD (first release)
    monitored            INTEGER NOT NULL DEFAULT 1,
    -- The chosen edition; release_locked = picked by a person (never changed automatically).
    release_id           INTEGER REFERENCES album_releases (id) ON DELETE SET NULL,
    release_locked       INTEGER NOT NULL DEFAULT 0,
    -- Kept although the metadata profile no longer wants it (it has files).
    filtered             INTEGER NOT NULL DEFAULT 0,
    path                 TEXT    NOT NULL DEFAULT '',   -- folder once files are imported
    added_at             INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);
INSERT INTO albums_new (id, artist_id, mbid, title, sort_title, disambiguation, primary_type, secondary_types, release_date, monitored,
    release_id, release_locked, filtered, path, added_at, updated_at)
SELECT id, artist_id, mbid, title, sort_title, disambiguation, primary_type, secondary_types, release_date, monitored,
    release_id, release_locked, filtered, path, added_at, updated_at FROM albums;
DROP TABLE albums;
ALTER TABLE albums_new RENAME TO albums;
CREATE INDEX albums_artist ON albums (artist_id, release_date);
CREATE INDEX albums_release_date ON albums (release_date) WHERE release_date <> '';
CREATE INDEX albums_monitored ON albums (monitored, artist_id);

-- adult_items
CREATE TABLE adult_items_new (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
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
INSERT INTO adult_items_new (id, kind, studio_id, studio_name, title, sort_title, release_date, year, duration, overview, stashdb_id, tpdb_id,
    phash, source_tags, image, monitored, quality_profile_id, root_folder_id, path, tags, added_at, updated_at)
SELECT id, kind, studio_id, studio_name, title, sort_title, release_date, year, duration, overview, stashdb_id, tpdb_id,
    phash, source_tags, image, monitored, quality_profile_id, root_folder_id, path, tags, added_at, updated_at FROM adult_items;
DROP TABLE adult_items;
ALTER TABLE adult_items_new RENAME TO adult_items;
CREATE UNIQUE INDEX adult_items_stashdb ON adult_items (stashdb_id) WHERE stashdb_id <> '';
CREATE UNIQUE INDEX adult_items_tpdb ON adult_items (tpdb_id) WHERE tpdb_id <> '';
CREATE INDEX adult_items_kind_sort ON adult_items (kind, sort_title, id);
CREATE INDEX adult_items_studio_date ON adult_items (studio_id, release_date);
CREATE INDEX adult_items_date ON adult_items (release_date);
CREATE INDEX adult_items_monitored ON adult_items (monitored, kind);

-- Next ids start above every id an item or a ref ever had.
DELETE FROM sqlite_sequence WHERE name IN ('media_items', 'seasons', 'episodes', 'albums', 'adult_items');
INSERT INTO sqlite_sequence (name, seq) VALUES
    ('media_items', max(COALESCE((SELECT MAX(id) FROM media_items), 0),
        COALESCE((SELECT MAX(id) FROM h17_refs WHERE kind IN ('movie', 'series', 'artist')), 0))),
    ('seasons', max(COALESCE((SELECT MAX(id) FROM seasons), 0), COALESCE((SELECT MAX(id) FROM h17_refs WHERE kind = 'season'), 0))),
    ('episodes', max(COALESCE((SELECT MAX(id) FROM episodes), 0), COALESCE((SELECT MAX(id) FROM h17_refs WHERE kind = 'episode'), 0))),
    ('albums', max(COALESCE((SELECT MAX(id) FROM albums), 0), COALESCE((SELECT MAX(id) FROM h17_refs WHERE kind = 'album'), 0))),
    ('adult_items', max(COALESCE((SELECT MAX(id) FROM adult_items), 0),
        COALESCE((SELECT MAX(id) FROM h17_refs WHERE kind IN ('scene', 'adult_movie')), 0)));

-- Nothing may point at a row that is not there: the CHECK fails (and the
-- transaction with it) if the rebuild broke a reference.
CREATE TEMP TABLE h17_fk_check (problems INTEGER NOT NULL CHECK (problems = 0));
INSERT INTO h17_fk_check (problems) SELECT
    (SELECT COUNT(*) FROM pragma_foreign_key_check('movies')) + (SELECT COUNT(*) FROM pragma_foreign_key_check('series')) +
    (SELECT COUNT(*) FROM pragma_foreign_key_check('artists')) + (SELECT COUNT(*) FROM pragma_foreign_key_check('media_files')) +
    (SELECT COUNT(*) FROM pragma_foreign_key_check('seasons')) + (SELECT COUNT(*) FROM pragma_foreign_key_check('episodes')) +
    (SELECT COUNT(*) FROM pragma_foreign_key_check('albums')) + (SELECT COUNT(*) FROM pragma_foreign_key_check('album_releases')) +
    (SELECT COUNT(*) FROM pragma_foreign_key_check('album_files')) + (SELECT COUNT(*) FROM pragma_foreign_key_check('subtitle_item_profiles')) +
    (SELECT COUNT(*) FROM pragma_foreign_key_check('adult_items')) + (SELECT COUNT(*) FROM pragma_foreign_key_check('adult_item_performers')) +
    (SELECT COUNT(*) FROM pragma_foreign_key_check('adult_files'));

DROP TABLE h17_fk_check;
DROP TABLE h17_refs;
DROP TABLE h17_items;

COMMIT;

PRAGMA foreign_keys = ON;

-- +goose Down
-- Ids stay non-reusable (the AUTOINCREMENT tables are left as they are);
-- only the title snapshots go. Detached refs keep their "#removed" mark.
ALTER TABLE downloads DROP COLUMN item_title;
ALTER TABLE timeline_events DROP COLUMN item_title;
