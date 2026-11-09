-- jusdoit schema
-- IDs are random hex strings to avoid enumeration in URLs.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user    ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS boards (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_boards_owner ON boards(owner_id);

CREATE TABLE IF NOT EXISTS board_members (
    board_id   TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (board_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_members_user ON board_members(user_id);

CREATE TABLE IF NOT EXISTS tasks (
    id         TEXT PRIMARY KEY,
    board_id   TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    done_at    TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_board_active ON tasks(board_id, done_at);

CREATE TABLE IF NOT EXISTS invites (
    id          TEXT PRIMARY KEY,
    board_id    TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    role        TEXT NOT NULL CHECK (role IN ('editor', 'viewer')),
    created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TEXT NOT NULL,
    accepted_at TEXT,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_invites_board ON invites(board_id);

-- Shared links that let people join a board without creating an account.
CREATE TABLE IF NOT EXISTS guest_links (
    id         TEXT PRIMARY KEY,
    board_id   TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    role       TEXT NOT NULL CHECK (role IN ('editor', 'viewer')),
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_guest_links_board ON guest_links(board_id);

-- Guests created through a link, so revoking it can kick those sessions.
CREATE TABLE IF NOT EXISTS guest_link_joins (
    link_id TEXT NOT NULL REFERENCES guest_links(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (link_id, user_id)
);
