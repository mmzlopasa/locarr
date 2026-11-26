-- +goose NO TRANSACTION
-- +goose Up
-- Adult image sets (galleries, D-112): a third kind of adult item next to
-- scenes and movies, with the ref "adult_gallery:<id>". A gallery may be
-- linked to the scene it belongs to (scene_id); its file row is the gallery
-- folder with the image count and total size. Performers can ask for the
-- photo set of every new scene ("galleries"). The "not interested" list
-- learns the gallery kind, keyed per kind.
--
-- adult_items and adult_exclusions are rebuilt to widen their kind CHECK
-- (SQLite's documented rebuild: foreign keys off, one transaction, checked
-- before commit), so this runs outside goose's transaction. Ids keep their
-- AUTOINCREMENT sequence (D-107).
PRAGMA foreign_keys = OFF;

BEGIN IMMEDIATE;

CREATE TEMP TABLE g20_seq (name TEXT PRIMARY KEY, seq INTEGER NOT NULL);
INSERT INTO g20_seq (name, seq) SELECT name, seq FROM sqlite_sequence WHERE name IN ('adult_items', 'adult_exclusions');

CREATE TABLE adult_items_new (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    kind                  TEXT    NOT NULL CHECK (kind IN ('scene', 'movie', 'gallery')),
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
    path                  TEXT    NOT NULL,   -- folder the file goes to (a gallery: its own folder)
    tags                  TEXT    NOT NULL DEFAULT '[]',
    added_at              INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL,
    -- galleries: the scene the photo set belongs to
    scene_id              INTEGER REFERENCES adult_items (id) ON DELETE SET NULL
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
CREATE INDEX adult_items_added ON adult_items (added_at, id);
CREATE INDEX adult_items_scene ON adult_items (scene_id) WHERE scene_id IS NOT NULL;

CREATE TABLE adult_exclusions_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    kind          TEXT    NOT NULL CHECK (kind IN ('scene', 'movie', 'gallery')),
    stashdb_id    TEXT    NOT NULL DEFAULT '',
    tpdb_id       TEXT    NOT NULL DEFAULT '',
    title         TEXT    NOT NULL,
    studio_name   TEXT    NOT NULL DEFAULT '',
    release_date  TEXT    NOT NULL DEFAULT '',
    added_at      INTEGER NOT NULL,
    CHECK (stashdb_id <> '' OR tpdb_id <> '')
);
INSERT INTO adult_exclusions_new (id, kind, stashdb_id, tpdb_id, title, studio_name, release_date, added_at)
SELECT id, kind, stashdb_id, tpdb_id, title, studio_name, release_date, added_at FROM adult_exclusions;
DROP TABLE adult_exclusions;
ALTER TABLE adult_exclusions_new RENAME TO adult_exclusions;
-- A gallery exclusion carries the source ids of its scene, so the ids are
-- unique per kind (scene and movie share one namespace, as before).
CREATE UNIQUE INDEX adult_exclusions_stashdb ON adult_exclusions (kind = 'gallery', stashdb_id) WHERE stashdb_id <> '';
CREATE UNIQUE INDEX adult_exclusions_tpdb ON adult_exclusions (kind = 'gallery', tpdb_id) WHERE tpdb_id <> '';
CREATE INDEX adult_exclusions_added ON adult_exclusions (added_at DESC, id DESC);

-- sqlite_sequence has no key on name: raise the existing row, add a
-- missing one (a rebuilt table without rows has none).
UPDATE sqlite_sequence SET seq = max(seq, (SELECT g.seq FROM g20_seq g WHERE g.name = sqlite_sequence.name))
WHERE name IN (SELECT name FROM g20_seq);
INSERT INTO sqlite_sequence (name, seq)
SELECT g.name, g.seq FROM g20_seq g WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence s WHERE s.name = g.name);

-- A gallery's file row: how many images and (in size) their total.
ALTER TABLE adult_files ADD COLUMN image_count INTEGER NOT NULL DEFAULT 0;
-- A followed performer can ask for the photo set of every new scene.
ALTER TABLE adult_performers ADD COLUMN galleries INTEGER NOT NULL DEFAULT 0;

CREATE TEMP TABLE g20_fk_check (problems INTEGER NOT NULL CHECK (problems = 0));
INSERT INTO g20_fk_check (problems) SELECT
    (SELECT COUNT(*) FROM pragma_foreign_key_check('adult_items')) + (SELECT COUNT(*) FROM pragma_foreign_key_check('adult_item_performers')) +
    (SELECT COUNT(*) FROM pragma_foreign_key_check('adult_files'));
DROP TABLE g20_fk_check;
DROP TABLE g20_seq;

COMMIT;

PRAGMA foreign_keys = ON;

-- +goose Down
-- Galleries cannot be represented by the older schema: they are removed.
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;
DELETE FROM adult_items WHERE kind = 'gallery';
DELETE FROM adult_exclusions WHERE kind = 'gallery';
ALTER TABLE adult_performers DROP COLUMN galleries;
ALTER TABLE adult_files DROP COLUMN image_count;
COMMIT;
PRAGMA foreign_keys = ON;
