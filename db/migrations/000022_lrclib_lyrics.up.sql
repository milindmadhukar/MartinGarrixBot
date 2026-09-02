-- Lyrics have only ever arrived by hand, through `make psql`. 79 rows out of 1348
-- have them, which is the pool the quiz has been drawing from since it was written.
--
-- LRCLIB (https://lrclib.net) is a keyless, registration-free community lyrics
-- database with good coverage of this catalogue. Only its plainLyrics is stored:
-- songs.lyrics is what /lyrics renders and what the quiz splits on newlines, and a
-- second column holding the same words with timestamps would be a second source of
-- truth with nothing reading it.

-- The LRCLIB record the words came from. NULL means hand-entered -- which is what
-- dedupe-songs treats as the most precious thing on a row, since it exists nowhere
-- else -- and it is also what makes an automatic fill reversible.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS lrclib_id BIGINT;

-- When LRCLIB was last asked about this row, stamped on every attempt whether it
-- found anything or not -- the same discipline announced_at uses. NULL means "never
-- asked", not "asked and got nothing", and that distinction is what stops the backlog
-- being walked in full on every cycle forever.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS lrclib_checked_at TIMESTAMPTZ;

-- Consecutive genuine misses, driving an exponential retry (7 days, 28, 112, 448,
-- then never). Incremented only when LRCLIB answered and had nothing usable -- never
-- on a timeout, a 5xx or a 429, because those say nothing about the row and would
-- retire it for the wrong reason.
--
-- A flat "never retry" would be wrong: LRCLIB is community-contributed and does grow.
-- A flat retry interval would be wrong too: most of this backlog is unreleased demos
-- and label B-sides that nobody is ever going to transcribe.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS lrclib_misses SMALLINT NOT NULL DEFAULT 0;
