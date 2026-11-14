CREATE TABLE auth_sessions (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    credential_hash BLOB NOT NULL CHECK (length(credential_hash) = 32),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX auth_sessions_expires_at_idx ON auth_sessions(expires_at);
