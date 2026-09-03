-- Schema drift repair.
--
-- The live database carries a UNIQUE CONSTRAINT uq_message_channel_author on
-- (message_id, channel_id, author_id), but no migration ever created it -- it was
-- applied by hand. So every database built from migrations alone (CI, the
-- integration tests, any fresh deploy) has no unique key on messages at all, and
-- MessageSent's ON CONFLICT DO NOTHING is dead code there: a redelivered gateway
-- event inserts a duplicate row and moves messages_sent a second time.
--
-- IF NOT EXISTS makes this a no-op against the live database, where a unique
-- index of that name already backs the existing constraint. Postgres builds an
-- index for a UNIQUE CONSTRAINT under the constraint's own name, so the names
-- collide exactly as intended.
--
-- No de-duplication pass runs first, deliberately. The only databases holding
-- message rows are the ones that already have the constraint, so there is
-- nothing to clean; a fresh database is empty and indexes instantly. That keeps
-- this migration off the 500k-row table it would otherwise have to scan.
CREATE UNIQUE INDEX IF NOT EXISTS uq_message_channel_author
    ON messages (message_id, channel_id, author_id);
