-- Remove moderator_role from guilds
ALTER TABLE guilds DROP COLUMN IF EXISTS moderator_role;
