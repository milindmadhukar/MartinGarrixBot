DROP TABLE IF EXISTS guild_configurations;

ALTER TABLE messages DROP COLUMN IF EXISTS guild_id;

ALTER TABLE users DROP COLUMN IF EXISTS guild_id;

-- Remove the composite primary key
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_pkey;

-- Restore the original primary key
ALTER TABLE users ADD CONSTRAINT users_pkey PRIMARY KEY (id);

-- Restore the unique constraint on id
ALTER TABLE users ADD CONSTRAINT users_id_key UNIQUE (id);