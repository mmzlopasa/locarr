-- +goose Up
-- Adult "not interested" list (D-110): a scene or movie the user removed
-- with "don't add it again" is remembered by its source IDs, so refreshing
-- a followed studio or performer never brings it back. Removing the entry
-- (Settings -> Adult content) or adding the item by hand undoes it.
CREATE TABLE adult_exclusions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    kind          TEXT    NOT NULL CHECK (kind IN ('scene', 'movie')),
    stashdb_id    TEXT    NOT NULL DEFAULT '',
    tpdb_id       TEXT    NOT NULL DEFAULT '',
    title         TEXT    NOT NULL,
    studio_name   TEXT    NOT NULL DEFAULT '',
    release_date  TEXT    NOT NULL DEFAULT '',
    added_at      INTEGER NOT NULL,
    CHECK (stashdb_id <> '' OR tpdb_id <> '')
);
CREATE UNIQUE INDEX adult_exclusions_stashdb ON adult_exclusions (stashdb_id) WHERE stashdb_id <> '';
CREATE UNIQUE INDEX adult_exclusions_tpdb ON adult_exclusions (tpdb_id) WHERE tpdb_id <> '';
CREATE INDEX adult_exclusions_added ON adult_exclusions (added_at DESC, id DESC);

-- The adult lists sort by date added too.
CREATE INDEX adult_items_added ON adult_items (added_at, id);

-- +goose Down
DROP INDEX adult_items_added;
DROP TABLE adult_exclusions;
