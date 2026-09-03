DROP INDEX IF EXISTS idx_songs_search;
ALTER TABLE songs DROP COLUMN IF EXISTS search_text;
ALTER TABLE songs DROP COLUMN IF EXISTS locked_fields;
