-- Drops the uniqueness only if THIS migration is what created it.
--
-- On the live database the name belongs to a UNIQUE CONSTRAINT applied by hand
-- long before this migration existed, and the up direction was a no-op there.
-- The honest inverse of a no-op is a no-op, so a pre-existing constraint is left
-- alone; only the bare index that a fresh database got from the up direction is
-- removed. DROP INDEX cannot drop a constraint's index anyway -- it errors, and
-- that error is what leaves a migration dirty mid-rollback.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'uq_message_channel_author'
    ) THEN
        RAISE NOTICE 'uq_message_channel_author is a pre-existing constraint; leaving it in place';
    ELSE
        DROP INDEX IF EXISTS uq_message_channel_author;
    END IF;
END $$;
