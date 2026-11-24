-- +goose Up
-- Music discovery: similar artists of the library's artists from several
-- providers, the suggested artists' names, tags and covers, and the artists
-- nobody wants suggested again.

-- When each library artist was last asked of each provider.
CREATE TABLE discovery_seeds (
    seed_mbid  TEXT    NOT NULL,
    provider   TEXT    NOT NULL,          -- lastfm, listenbrainz, musicbrainz, spotify
    fetched_at INTEGER NOT NULL,
    error      TEXT    NOT NULL DEFAULT '', -- the last failure ('' = fine)
    PRIMARY KEY (seed_mbid, provider)
) WITHOUT ROWID;

-- One provider's answer: target is similar to seed with score (0..1); the
-- confidence (0..1) says how sure the MusicBrainz id of the target is.
CREATE TABLE discovery_similar (
    seed_mbid   TEXT NOT NULL,
    provider    TEXT NOT NULL,
    target_mbid TEXT NOT NULL,
    score       REAL NOT NULL,
    confidence  REAL NOT NULL,
    PRIMARY KEY (seed_mbid, provider, target_mbid)
) WITHOUT ROWID;
CREATE INDEX discovery_similar_target ON discovery_similar (target_mbid);

-- What is known of a suggested artist.
CREATE TABLE discovery_artists (
    mbid           TEXT    PRIMARY KEY,
    name           TEXT    NOT NULL,
    disambiguation TEXT    NOT NULL DEFAULT '',
    tags           TEXT    NOT NULL DEFAULT '[]', -- JSON list, most used first
    cover_group    TEXT    NOT NULL DEFAULT '',   -- release group whose front cover stands for the artist
    lastfm_url     TEXT    NOT NULL DEFAULT '',
    spotify_id     TEXT    NOT NULL DEFAULT '',
    enriched_at    INTEGER,                       -- tags and cover looked up
    updated_at     INTEGER NOT NULL
) WITHOUT ROWID;

-- "Not interested".
CREATE TABLE discovery_exclusions (
    mbid       TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL DEFAULT '',
    user_id    INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE discovery_exclusions;
DROP TABLE discovery_artists;
DROP INDEX discovery_similar_target;
DROP TABLE discovery_similar;
DROP TABLE discovery_seeds;
