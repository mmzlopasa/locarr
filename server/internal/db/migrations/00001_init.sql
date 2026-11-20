-- +goose Up
CREATE TABLE users (
    id              INTEGER PRIMARY KEY,
    username        TEXT    NOT NULL,
    username_key    TEXT    NOT NULL UNIQUE, -- lower-cased for case-insensitive lookup
    password_hash   TEXT    NOT NULL,
    role            TEXT    NOT NULL CHECK (role IN ('admin', 'manager', 'requester')),
    permissions     INTEGER NOT NULL DEFAULT 0,
    language        TEXT    NOT NULL DEFAULT 'en',
    disabled        INTEGER NOT NULL DEFAULT 0,
    failed_logins   INTEGER NOT NULL DEFAULT 0,
    locked_until    INTEGER,
    totp_secret     BLOB,            -- encrypted with the secret key
    totp_pending    BLOB,            -- encrypted; set between setup and enable
    totp_enabled    INTEGER NOT NULL DEFAULT 0,
    totp_last_step  INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    last_login_at   INTEGER
);

CREATE TABLE recovery_codes (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  BLOB    NOT NULL,
    used_at    INTEGER
);
CREATE INDEX recovery_codes_user ON recovery_codes (user_id);

CREATE TABLE sessions (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash    BLOB    NOT NULL UNIQUE,
    device_name   TEXT    NOT NULL,
    client        TEXT    NOT NULL,
    ip            TEXT    NOT NULL,
    created_at    INTEGER NOT NULL,
    last_seen_at  INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expires ON sessions (expires_at);

CREATE TABLE api_keys (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          TEXT    NOT NULL,
    prefix        TEXT    NOT NULL,
    key_hash      BLOB    NOT NULL UNIQUE,
    read_only     INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER
);
CREATE INDEX api_keys_user ON api_keys (user_id);

-- Plain settings: JSON values.
CREATE TABLE settings (
    key         TEXT    PRIMARY KEY,
    value       TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- Third-party credentials, AES-GCM encrypted with the key in the config folder.
CREATE TABLE secrets (
    key         TEXT    PRIMARY KEY,
    ciphertext  BLOB    NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE root_folders (
    id          INTEGER PRIMARY KEY,
    path        TEXT    NOT NULL UNIQUE,
    kind        TEXT    NOT NULL CHECK (kind IN ('movies', 'series', 'music')),
    created_at  INTEGER NOT NULL
);

CREATE TABLE jobs (
    id                INTEGER PRIMARY KEY,
    kind              TEXT    NOT NULL,
    queue             TEXT    NOT NULL,
    args              TEXT    NOT NULL DEFAULT '{}',
    unique_key        TEXT,
    state             TEXT    NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    attempt           INTEGER NOT NULL DEFAULT 0,
    max_attempts      INTEGER NOT NULL,
    run_at            INTEGER NOT NULL,
    created_at        INTEGER NOT NULL,
    started_at        INTEGER,
    finished_at       INTEGER,
    last_error        TEXT,
    cancel_requested  INTEGER NOT NULL DEFAULT 0
);
-- At most one queued or running job per unique key.
CREATE UNIQUE INDEX jobs_unique_active ON jobs (unique_key)
    WHERE unique_key IS NOT NULL AND state IN ('queued', 'running');
CREATE INDEX jobs_due ON jobs (queue, run_at) WHERE state = 'queued';
CREATE INDEX jobs_running ON jobs (state) WHERE state = 'running';
CREATE INDEX jobs_finished ON jobs (finished_at) WHERE finished_at IS NOT NULL;
CREATE INDEX jobs_kind ON jobs (kind, id);

CREATE TABLE job_schedules (
    name              TEXT    PRIMARY KEY,
    kind              TEXT    NOT NULL,
    interval_seconds  INTEGER NOT NULL,
    next_run_at       INTEGER NOT NULL,
    last_run_at       INTEGER,
    last_job_id       INTEGER
);

-- +goose Down
DROP TABLE job_schedules;
DROP TABLE jobs;
DROP TABLE root_folders;
DROP TABLE secrets;
DROP TABLE settings;
DROP TABLE api_keys;
DROP TABLE sessions;
DROP TABLE recovery_codes;
DROP TABLE users;
