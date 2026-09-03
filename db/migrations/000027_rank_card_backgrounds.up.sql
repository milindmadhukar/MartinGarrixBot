-- Rank card backgrounds move from baked-in template PNGs to a shared,
-- uploadable catalogue. A guild picks a subset of the catalogue (or none, to
-- mean "the whole catalogue") and a selection mode.
CREATE TABLE backgrounds (
    id BIGSERIAL PRIMARY KEY,
    filename TEXT NOT NULL UNIQUE,
    -- NULL for the built-in backgrounds seeded at startup, which nobody
    -- uploaded.
    uploaded_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which backgrounds a guild has opted into. An empty set (no rows for a
-- guild_id) means "use the whole catalogue" -- so a guild that never visits
-- this settings page keeps the pre-feature behaviour rather than rendering
-- with no background at all.
CREATE TABLE guild_backgrounds (
    guild_id BIGINT NOT NULL REFERENCES guilds(guild_id) ON DELETE CASCADE,
    background_id BIGINT NOT NULL REFERENCES backgrounds(id) ON DELETE CASCADE,
    PRIMARY KEY (guild_id, background_id)
);

ALTER TABLE guilds
    ADD COLUMN background_mode TEXT NOT NULL DEFAULT 'random'
        CHECK (background_mode IN ('random', 'cycle')),
    -- Remembers the last background used in cycle mode, so the next /rank
    -- render can advance to the next one in id order rather than picking
    -- randomly. NULL (never cycled yet, or the remembered id fell out of the
    -- selection) means "start from the beginning."
    ADD COLUMN background_cycle_background_id BIGINT
        REFERENCES backgrounds(id) ON DELETE SET NULL;
