-- +goose Up
-- P6: music on the shared library core (D-028). An artist is a media_items
-- row (kind 'artist') with its details in `artists`; albums are MusicBrainz
-- release groups of the artist, each with its known releases (editions) and
-- the track list of the chosen release. Files are media_files rows of the
-- artist, linked to an album (and through disc/position or recording id to a
-- track) by `album_files`.

-- Which albums of an artist the library tracks (D-041).
CREATE TABLE metadata_profiles (
    id               INTEGER PRIMARY KEY,
    name             TEXT    NOT NULL UNIQUE,
    primary_types    TEXT    NOT NULL DEFAULT '["album"]',     -- JSON: album, ep, single, broadcast, other
    secondary_types  TEXT    NOT NULL DEFAULT '["studio"]',    -- JSON: studio (= none), compilation, live, …
    release_statuses TEXT    NOT NULL DEFAULT '["official"]',  -- JSON: official, promotion, bootleg, pseudo-release, …
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
INSERT INTO metadata_profiles (name, primary_types, secondary_types, release_statuses, created_at, updated_at) VALUES
    ('Standard', '["album","ep"]', '["studio"]', '["official"]', 0, 0),
    ('Albums only', '["album"]', '["studio"]', '["official"]', 0, 0),
    ('Everything', '["album","ep","single","broadcast","other"]',
     '["studio","compilation","soundtrack","spokenword","interview","audiobook","audio drama","live","remix","dj-mix","mixtape/street","demo","field recording"]',
     '["official","promotion","bootleg","pseudo-release"]', 0, 0);

CREATE TABLE artists (
    item_id               INTEGER PRIMARY KEY REFERENCES media_items (id) ON DELETE CASCADE,
    mbid                  TEXT    NOT NULL UNIQUE,
    sort_name             TEXT    NOT NULL DEFAULT '',
    disambiguation        TEXT    NOT NULL DEFAULT '',
    type                  TEXT    NOT NULL DEFAULT '',   -- Person, Group, …
    country               TEXT    NOT NULL DEFAULT '',
    aliases               TEXT    NOT NULL DEFAULT '[]', -- JSON list of names
    genres                TEXT    NOT NULL DEFAULT '[]',
    metadata_profile_id   INTEGER NOT NULL REFERENCES metadata_profiles (id),
    monitor_new_albums    TEXT    NOT NULL DEFAULT 'all' CHECK (monitor_new_albums IN ('all', 'none', 'future')),
    metadata_refreshed_at INTEGER
);

-- Albums = release groups.
CREATE TABLE albums (
    id                   INTEGER PRIMARY KEY,
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
CREATE INDEX albums_artist ON albums (artist_id, release_date);
CREATE INDEX albums_release_date ON albums (release_date) WHERE release_date <> '';
CREATE INDEX albums_monitored ON albums (monitored, artist_id);

-- Editions of an album; tracks are loaded for the chosen one (and on demand).
CREATE TABLE album_releases (
    id              INTEGER PRIMARY KEY,
    album_id        INTEGER NOT NULL REFERENCES albums (id) ON DELETE CASCADE,
    mbid            TEXT    NOT NULL UNIQUE,
    title           TEXT    NOT NULL,
    status          TEXT    NOT NULL DEFAULT '',
    release_date    TEXT    NOT NULL DEFAULT '',
    country         TEXT    NOT NULL DEFAULT '',
    disambiguation  TEXT    NOT NULL DEFAULT '',
    label           TEXT    NOT NULL DEFAULT '',
    catalog_number  TEXT    NOT NULL DEFAULT '',
    barcode         TEXT    NOT NULL DEFAULT '',
    formats         TEXT    NOT NULL DEFAULT '[]',   -- JSON, one per medium
    track_count     INTEGER NOT NULL DEFAULT 0,
    media_count     INTEGER NOT NULL DEFAULT 0,
    tracks_loaded   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX album_releases_album ON album_releases (album_id);

CREATE TABLE tracks (
    id              INTEGER PRIMARY KEY,
    release_id      INTEGER NOT NULL REFERENCES album_releases (id) ON DELETE CASCADE,
    mbid            TEXT    NOT NULL DEFAULT '',
    recording_mbid  TEXT    NOT NULL DEFAULT '',
    disc            INTEGER NOT NULL,
    position        INTEGER NOT NULL,
    number          TEXT    NOT NULL DEFAULT '',
    title           TEXT    NOT NULL,
    length_ms       INTEGER NOT NULL DEFAULT 0,
    artist          TEXT    NOT NULL DEFAULT '',
    UNIQUE (release_id, disc, position)
);
CREATE INDEX tracks_recording ON tracks (recording_mbid) WHERE recording_mbid <> '';

-- A library file of an album: which disc and position of the album it is
-- (and the recording, so a change of edition finds it again) and what its tags said.
CREATE TABLE album_files (
    file_id         INTEGER PRIMARY KEY REFERENCES media_files (id) ON DELETE CASCADE,
    album_id        INTEGER NOT NULL REFERENCES albums (id) ON DELETE CASCADE,
    disc            INTEGER NOT NULL DEFAULT 1,
    position        INTEGER NOT NULL DEFAULT 0,
    recording_mbid  TEXT    NOT NULL DEFAULT '',
    title           TEXT    NOT NULL DEFAULT '',
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    format          TEXT    NOT NULL DEFAULT '',
    bit_depth       INTEGER NOT NULL DEFAULT 0,
    sample_rate     INTEGER NOT NULL DEFAULT 0,
    bitrate         INTEGER NOT NULL DEFAULT 0,
    tags            TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX album_files_album ON album_files (album_id, disc, position);

-- +goose Down
DROP TABLE album_files;
DROP TABLE tracks;
DROP TABLE album_releases;
DROP TABLE albums;
DROP TABLE artists;
DROP TABLE metadata_profiles;
