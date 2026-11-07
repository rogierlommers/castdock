ALTER TABLE episodes
    ADD COLUMN state TEXT NOT NULL DEFAULT 'archived'
    CHECK (state IN ('inbox', 'queued', 'archived'));

ALTER TABLE episodes
    ADD COLUMN queue_position INTEGER;

CREATE INDEX episodes_state_published_idx
    ON episodes(state, published_at DESC, id DESC);

CREATE UNIQUE INDEX episodes_queue_position_idx
    ON episodes(queue_position)
    WHERE queue_position IS NOT NULL;
