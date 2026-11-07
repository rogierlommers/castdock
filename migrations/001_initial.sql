CREATE TABLE feeds (
    id INTEGER PRIMARY KEY,
    url TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    site_url TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL DEFAULT '',
    etag TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT '',
    last_checked TEXT,
    last_success TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE episodes (
    id INTEGER PRIMARY KEY,
    feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    published_at TEXT,
    duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),
    enclosure_url TEXT NOT NULL DEFAULT '',
    enclosure_type TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(feed_id, guid)
);

CREATE INDEX episodes_feed_published_idx
    ON episodes(feed_id, published_at DESC, id DESC);

CREATE TABLE playback (
    episode_id INTEGER PRIMARY KEY REFERENCES episodes(id) ON DELETE CASCADE,
    position_seconds REAL NOT NULL DEFAULT 0 CHECK (position_seconds >= 0),
    duration_seconds REAL NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),
    completed INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0, 1)),
    playing INTEGER NOT NULL DEFAULT 0 CHECK (playing IN (0, 1)),
    client_updated_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
