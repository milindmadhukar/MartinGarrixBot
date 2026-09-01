-- Beatport track pages are /track/<slug>/<id>. The bot only ever stored the id and
-- built /track/<id>, which is not a route Beatport serves: every Beatport button in
-- the catalogue led to a 404.
--
-- The slug cannot be derived from what we store. Beatport slugifies the track's own
-- name including its feature credit -- "X's feat. Icona Pop" becomes xs-feat-icona-pop
-- -- while this catalogue deliberately strips features into the artist column, so the
-- local title reduces to "xs". It has to come from the API, which returns it on the
-- same listing responses the sync already reads.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS beatport_slug TEXT;
