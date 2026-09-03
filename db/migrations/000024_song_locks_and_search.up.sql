-- A field an owner has corrected by hand in the dashboard.
--
-- Every automated writer skips a column named here, so the 15-minute STMPD and
-- Beatport syncs, the hourly Apple enrichment and the daily LRCLIB sweep cannot undo a
-- correction on their next cycle. That is what made correcting this table by hand
-- pointless before: a human writes a value once, and four tickers rewrite the same
-- columns forever.
--
-- A set of column names rather than a boolean per column: the set is sparse -- a
-- handful of rows will ever hold one -- and the alternative is twenty-two more columns
-- on a table that already has forty-one. Per-field rather than one whole-row flag so a
-- hand-fixed cover does not also freeze link enrichment on the same song.
--
-- Which names are legal is enforced in Go (utils/catalogue.LockableFields), not by a
-- CHECK, so that making a column editable is not a migration.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS locked_fields TEXT[] NOT NULL DEFAULT '{}';

-- The folded haystack a row is found by: artists, title, rendition and release name,
-- lowercased with diacritics stripped, apostrophes dropped and punctuation collapsed to
-- single spaces.
--
-- It exists because the old search was one contiguous LIKE over
-- `artists || ' - ' || name`, which cannot find "Don't Tell Me" by "Matisse & Sadko,
-- Aspyer, Matluck" from "matisse sadko dont tell me" -- the terms are in the row but
-- not adjacent, and the table mixes ' with ’ so even the apostrophe has to match by
-- luck. Matching every token separately against a folded haystack finds it.
--
-- Deliberately not GENERATED, for the same reason as match_key in 000011 and
-- normalized_name in 000021: the folding rules live in utils/ because Go needs them
-- anyway to fold the query typed by the user, and two implementations would drift.
--
-- Nullable on purpose. NULL means "nobody has derived this yet", and every reader
-- coalesces to an unfolded expression over the same columns, so a row inserted between
-- a deploy and the next rekey-songs is still findable -- just without the folding.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS search_text TEXT;

CREATE INDEX IF NOT EXISTS idx_songs_search ON songs (search_text);
