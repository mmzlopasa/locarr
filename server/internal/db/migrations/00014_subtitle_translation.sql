-- +goose Up
-- Subtitle translation: translation services with encrypted keys, health and
-- a monthly cap; usage per service and month; machine-translated subtitles
-- in the subtitles table; per-profile "translate after N hours" and the
-- per-item translation schedule.

CREATE TABLE subtitle_translators (
    name             TEXT    PRIMARY KEY,   -- libretranslate, deepl, openai, anthropic, gemini, openai-compatible
    enabled          INTEGER NOT NULL DEFAULT 0,
    base_url         TEXT    NOT NULL DEFAULT '',
    api_key          BLOB,
    model            TEXT    NOT NULL DEFAULT '',
    formality        TEXT    NOT NULL DEFAULT '',
    glossary_id      TEXT    NOT NULL DEFAULT '',
    monthly_cap      INTEGER NOT NULL DEFAULT 0,   -- characters or tokens per month; 0 = no cap
    failures         INTEGER NOT NULL DEFAULT 0,
    retry_at         INTEGER,
    last_error       TEXT    NOT NULL DEFAULT '',
    last_error_code  TEXT    NOT NULL DEFAULT '',
    last_error_at    INTEGER,
    last_success_at  INTEGER,
    quota_used       INTEGER,                      -- what the service itself reports (DeepL)
    quota_limit      INTEGER,
    quota_checked_at INTEGER,
    updated_at       INTEGER NOT NULL
);
INSERT INTO subtitle_translators (name, updated_at) VALUES
    ('libretranslate', 0), ('deepl', 0), ('openai', 0), ('anthropic', 0), ('gemini', 0), ('openai-compatible', 0);

CREATE TABLE subtitle_translation_usage (
    translator     TEXT    NOT NULL,
    month          TEXT    NOT NULL,   -- 2026-11 (UTC)
    characters     INTEGER NOT NULL DEFAULT 0,
    input_tokens   INTEGER NOT NULL DEFAULT 0,
    output_tokens  INTEGER NOT NULL DEFAULT 0,
    requests       INTEGER NOT NULL DEFAULT 0,
    lines          INTEGER NOT NULL DEFAULT 0,
    subtitles      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (translator, month)
) WITHOUT ROWID;

ALTER TABLE subtitle_profiles ADD COLUMN translate_after_hours INTEGER NOT NULL DEFAULT 0;  -- 0 = never translate automatically

ALTER TABLE subtitle_state ADD COLUMN translate_at INTEGER;            -- when a missing language may be translated
ALTER TABLE subtitle_state ADD COLUMN translate_retry_at INTEGER;      -- after a failed translation
ALTER TABLE subtitle_state ADD COLUMN translate_attempts INTEGER NOT NULL DEFAULT 0;
CREATE INDEX subtitle_state_translate ON subtitle_state (translate_at) WHERE translate_at IS NOT NULL;

-- The subtitles table gets the source 'translated' (a CHECK cannot be
-- altered in SQLite, so the table is rebuilt) and what a translation came from.
CREATE TABLE subtitles_new (
    id                   INTEGER PRIMARY KEY,
    media_file_id        INTEGER NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    item_ref             TEXT    NOT NULL,
    path                 TEXT    NOT NULL UNIQUE,
    language             TEXT    NOT NULL,
    forced               INTEGER NOT NULL DEFAULT 0,
    hearing_impaired     INTEGER NOT NULL DEFAULT 0,
    format               TEXT    NOT NULL,
    source               TEXT    NOT NULL CHECK (source IN ('downloaded', 'disk', 'translated')),
    provider             TEXT    NOT NULL DEFAULT '',   -- for translations: the translation service
    provider_id          TEXT    NOT NULL DEFAULT '',
    release              TEXT    NOT NULL DEFAULT '',
    score                INTEGER,
    encoding             TEXT    NOT NULL DEFAULT '',
    sync_state           TEXT    NOT NULL DEFAULT '',
    sync_message         TEXT    NOT NULL DEFAULT '',
    sync_offset_ms       INTEGER NOT NULL DEFAULT 0,
    sync_scale           REAL    NOT NULL DEFAULT 1,
    sync_at              INTEGER,
    translated_from      TEXT    NOT NULL DEFAULT '',   -- source language of a translation
    translation_source   TEXT    NOT NULL DEFAULT '',   -- "subtitle:<file name>" or "track:<n>"
    translation_original TEXT    NOT NULL DEFAULT '[]', -- JSON: the first source lines (preview before/after)
    added_at             INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);
INSERT INTO subtitles_new (id, media_file_id, item_ref, path, language, forced, hearing_impaired, format, source, provider, provider_id,
    release, score, encoding, sync_state, sync_message, sync_offset_ms, sync_scale, sync_at, added_at, updated_at)
SELECT id, media_file_id, item_ref, path, language, forced, hearing_impaired, format, source, provider, provider_id,
    release, score, encoding, sync_state, sync_message, sync_offset_ms, sync_scale, sync_at, added_at, updated_at FROM subtitles;
DROP TABLE subtitles;
ALTER TABLE subtitles_new RENAME TO subtitles;
CREATE INDEX subtitles_file ON subtitles (media_file_id);
CREATE INDEX subtitles_item ON subtitles (item_ref);

-- +goose Down
DELETE FROM subtitles WHERE source = 'translated';
CREATE TABLE subtitles_old (
    id               INTEGER PRIMARY KEY,
    media_file_id    INTEGER NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    item_ref         TEXT    NOT NULL,
    path             TEXT    NOT NULL UNIQUE,
    language         TEXT    NOT NULL,
    forced           INTEGER NOT NULL DEFAULT 0,
    hearing_impaired INTEGER NOT NULL DEFAULT 0,
    format           TEXT    NOT NULL,
    source           TEXT    NOT NULL CHECK (source IN ('downloaded', 'disk')),
    provider         TEXT    NOT NULL DEFAULT '',
    provider_id      TEXT    NOT NULL DEFAULT '',
    release          TEXT    NOT NULL DEFAULT '',
    score            INTEGER,
    encoding         TEXT    NOT NULL DEFAULT '',
    sync_state       TEXT    NOT NULL DEFAULT '',
    sync_message     TEXT    NOT NULL DEFAULT '',
    sync_offset_ms   INTEGER NOT NULL DEFAULT 0,
    sync_scale       REAL    NOT NULL DEFAULT 1,
    sync_at          INTEGER,
    added_at         INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
INSERT INTO subtitles_old SELECT id, media_file_id, item_ref, path, language, forced, hearing_impaired, format, source, provider, provider_id,
    release, score, encoding, sync_state, sync_message, sync_offset_ms, sync_scale, sync_at, added_at, updated_at FROM subtitles;
DROP TABLE subtitles;
ALTER TABLE subtitles_old RENAME TO subtitles;
CREATE INDEX subtitles_file ON subtitles (media_file_id);
CREATE INDEX subtitles_item ON subtitles (item_ref);
DROP INDEX subtitle_state_translate;
ALTER TABLE subtitle_state DROP COLUMN translate_attempts;
ALTER TABLE subtitle_state DROP COLUMN translate_retry_at;
ALTER TABLE subtitle_state DROP COLUMN translate_at;
ALTER TABLE subtitle_profiles DROP COLUMN translate_after_hours;
DROP TABLE subtitle_translation_usage;
DROP TABLE subtitle_translators;
