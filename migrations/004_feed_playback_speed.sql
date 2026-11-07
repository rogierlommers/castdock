ALTER TABLE feeds
    ADD COLUMN playback_speed REAL NOT NULL DEFAULT 1.0
    CHECK (playback_speed >= 0.5 AND playback_speed <= 3.0);
