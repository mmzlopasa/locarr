-- +goose Up
-- P8: requests, issues, notifications, invites, quotas and reserved external
-- sign-in identities (D-054 … D-058).

-- A user's request for a title. kind: movie (tmdb_id), series (tmdb_id and
-- request_seasons), album (mbid = release group, artist_mbid) or artist (mbid).
-- 4K requests are a separate track of the same title (is_4k, D-055).
CREATE TABLE requests (
    id               INTEGER PRIMARY KEY,
    kind             TEXT    NOT NULL CHECK (kind IN ('movie', 'series', 'album', 'artist')),
    is_4k            INTEGER NOT NULL DEFAULT 0,
    status           TEXT    NOT NULL CHECK (status IN ('pending', 'approved', 'declined', 'cancelled', 'processing',
                                                        'partial', 'available', 'failed')),
    tmdb_id          INTEGER NOT NULL DEFAULT 0,
    mbid             TEXT    NOT NULL DEFAULT '',
    artist_mbid      TEXT    NOT NULL DEFAULT '',
    title            TEXT    NOT NULL,
    subtitle         TEXT    NOT NULL DEFAULT '',
    year             INTEGER NOT NULL DEFAULT 0,
    poster           TEXT    NOT NULL DEFAULT '',
    all_seasons      INTEGER NOT NULL DEFAULT 0,
    future_seasons   INTEGER NOT NULL DEFAULT 0,
    requested_by     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    decided_by       INTEGER REFERENCES users (id) ON DELETE SET NULL,
    auto_approved    INTEGER NOT NULL DEFAULT 0,
    decided_at       INTEGER,
    decline_reason   TEXT    NOT NULL DEFAULT '',
    -- Library options; NULL = the request defaults of the kind.
    profile_id       INTEGER,
    root_folder_id   INTEGER,
    tags             TEXT    NOT NULL DEFAULT '[]',
    -- The library item once approved ("movie:42", "series:7", "album:3", "artist:2").
    item_ref         TEXT    NOT NULL DEFAULT '',
    -- The library's explanation while it is on its way (timeline.Explain).
    explain_code     TEXT    NOT NULL DEFAULT '',
    explain_tone     TEXT    NOT NULL DEFAULT '',
    explain_message  TEXT    NOT NULL DEFAULT '',
    explain_params   TEXT    NOT NULL DEFAULT '{}',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    available_at     INTEGER
);
CREATE INDEX requests_status ON requests (status, created_at);
CREATE INDEX requests_user ON requests (requested_by, created_at);
CREATE INDEX requests_tmdb ON requests (kind, tmdb_id) WHERE tmdb_id <> 0;
CREATE INDEX requests_mbid ON requests (kind, mbid) WHERE mbid <> '';
CREATE INDEX requests_item ON requests (item_ref) WHERE item_ref <> '';

-- The seasons a series request asks for, each with its own state.
CREATE TABLE request_seasons (
    request_id  INTEGER NOT NULL REFERENCES requests (id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    status      TEXT    NOT NULL,
    PRIMARY KEY (request_id, number)
);

CREATE TABLE request_comments (
    id          INTEGER PRIMARY KEY,
    request_id  INTEGER NOT NULL REFERENCES requests (id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body        TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE INDEX request_comments_request ON request_comments (request_id, id);

-- A problem a user reports with an available title.
CREATE TABLE issues (
    id                   INTEGER PRIMARY KEY,
    kind                 TEXT    NOT NULL CHECK (kind IN ('movie', 'series', 'album')),
    type                 TEXT    NOT NULL CHECK (type IN ('video', 'audio', 'subtitles', 'other')),
    status               TEXT    NOT NULL CHECK (status IN ('open', 'resolved')),
    item_ref             TEXT    NOT NULL,
    tmdb_id              INTEGER NOT NULL DEFAULT 0,
    mbid                 TEXT    NOT NULL DEFAULT '',
    title                TEXT    NOT NULL,
    poster               TEXT    NOT NULL DEFAULT '',
    season_number        INTEGER,
    episode_number       INTEGER,
    message              TEXT    NOT NULL,
    reported_by          INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    resolved_by          INTEGER REFERENCES users (id) ON DELETE SET NULL,
    resolved_at          INTEGER,
    replacement_searched INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);
CREATE INDEX issues_status ON issues (status, created_at);
CREATE INDEX issues_user ON issues (reported_by, created_at);

CREATE TABLE issue_comments (
    id          INTEGER PRIMARY KEY,
    issue_id    INTEGER NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body        TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE INDEX issue_comments_issue ON issue_comments (issue_id, id);

-- In-app notifications, one row per recipient (D-057).
CREATE TABLE notifications (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type        TEXT    NOT NULL,
    title       TEXT    NOT NULL DEFAULT '',
    message     TEXT    NOT NULL DEFAULT '',
    poster      TEXT    NOT NULL DEFAULT '',
    media_kind  TEXT    NOT NULL DEFAULT '',
    actor_id    INTEGER REFERENCES users (id) ON DELETE SET NULL,
    request_id  INTEGER REFERENCES requests (id) ON DELETE CASCADE,
    issue_id    INTEGER REFERENCES issues (id) ON DELETE CASCADE,
    params      TEXT    NOT NULL DEFAULT '{}',
    created_at  INTEGER NOT NULL,
    read_at     INTEGER
);
CREATE INDEX notifications_user ON notifications (user_id, id);
CREATE INDEX notifications_unread ON notifications (user_id) WHERE read_at IS NULL;

-- Per-user quota rules (the defaults are a setting).
CREATE TABLE user_quotas (
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind        TEXT    NOT NULL CHECK (kind IN ('movie', 'series', 'music')),
    limit_count INTEGER NOT NULL,
    days        INTEGER NOT NULL,
    PRIMARY KEY (user_id, kind)
);

-- Invite links: the code is shown once and stored as SHA-256.
CREATE TABLE invites (
    id           INTEGER PRIMARY KEY,
    label        TEXT    NOT NULL DEFAULT '',
    code_hash    BLOB    NOT NULL UNIQUE,
    role         TEXT    NOT NULL CHECK (role IN ('manager', 'requester')),
    permissions  INTEGER NOT NULL,
    quotas       TEXT    NOT NULL DEFAULT '{}',
    max_uses     INTEGER NOT NULL DEFAULT 1,
    uses         INTEGER NOT NULL DEFAULT 0,
    expires_at   INTEGER,
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    revoked_at   INTEGER
);

CREATE TABLE invite_signups (
    invite_id   INTEGER NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (invite_id, user_id)
);

-- Reserved for sign-in through another service (Streamero, P9; D-058): one
-- external account maps to one local user.
CREATE TABLE user_identities (
    provider      TEXT    NOT NULL,
    subject       TEXT    NOT NULL,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    username      TEXT    NOT NULL DEFAULT '',
    linked_at     INTEGER NOT NULL,
    last_login_at INTEGER,
    PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user ON user_identities (user_id);

-- +goose Down
DROP TABLE user_identities;
DROP TABLE invite_signups;
DROP TABLE invites;
DROP TABLE user_quotas;
DROP TABLE notifications;
DROP TABLE issue_comments;
DROP TABLE issues;
DROP TABLE request_comments;
DROP TABLE request_seasons;
DROP TABLE requests;
