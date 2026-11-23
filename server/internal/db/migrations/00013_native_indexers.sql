-- +goose Up
-- Built-in public sites (native indexers). A native indexer keeps kind
-- 'torznab' (its protocol: torrent) and names its definition; the base URL is
-- the site address in use (a mirror can be picked).
ALTER TABLE indexers ADD COLUMN definition TEXT;                     -- definition id, NULL for Torznab/Newznab
ALTER TABLE indexers ADD COLUMN options TEXT NOT NULL DEFAULT '{}';  -- JSON per-definition settings
ALTER TABLE indexers ADD COLUMN parse_failures INTEGER NOT NULL DEFAULT 0; -- result rows that could not be read
ALTER TABLE indexers ADD COLUMN self_test_at INTEGER;                -- last weekly self-test
ALTER TABLE indexers ADD COLUMN self_test_ok INTEGER;
CREATE INDEX indexers_definition ON indexers (definition) WHERE definition IS NOT NULL;

-- +goose Down
DROP INDEX indexers_definition;
ALTER TABLE indexers DROP COLUMN self_test_ok;
ALTER TABLE indexers DROP COLUMN self_test_at;
ALTER TABLE indexers DROP COLUMN parse_failures;
ALTER TABLE indexers DROP COLUMN options;
ALTER TABLE indexers DROP COLUMN definition;
