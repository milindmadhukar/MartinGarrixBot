-- unique_release identified a song by (name, artists, release_date), which cannot
-- tell two renditions apart when they share a release day -- and remixes routinely do.
-- That forced the rendition to be smuggled into the name for some rows and left in
-- mix_name for others, so "Catharina (Remixes)" and "Catharina" with mix "Remixes"
-- were the same song written two different ways, and neither the matcher nor a human
-- reading the table could tell which was which.
--
-- With the rendition part of the identity, the name can be just the song's name.
ALTER TABLE songs DROP CONSTRAINT IF EXISTS unique_release;
ALTER TABLE songs ADD CONSTRAINT unique_release
    UNIQUE NULLS NOT DISTINCT (name, artists, mix_name, release_date);

-- Tracking parameters carry nothing a listener or the bot needs, and they make the
-- same link look like two. Strip them everywhere they appear.
UPDATE songs SET spotify_url = split_part(spotify_url, '?', 1)
 WHERE spotify_url LIKE '%?%';
UPDATE songs SET apple_music_url = split_part(apple_music_url, '?', 1)
 WHERE apple_music_url LIKE '%?%' AND apple_music_url NOT LIKE '%?i=%';
UPDATE songs SET deezer_url = split_part(deezer_url, '?', 1)
 WHERE deezer_url LIKE '%?%';
UPDATE songs SET tidal_url = split_part(tidal_url, '?', 1)
 WHERE tidal_url LIKE '%?%';
UPDATE songs SET amazon_music_url = split_part(amazon_music_url, '?', 1)
 WHERE amazon_music_url LIKE '%?%';
