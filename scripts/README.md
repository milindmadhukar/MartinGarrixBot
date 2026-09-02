# Maintenance scripts

One-off passes over the `songs` table. They are separate binaries rather than flags
on the bot for two reasons: a backfill rewrites release dates and deletes merged
duplicate rows, which is not something that should be reachable from a process that
runs unattended for weeks; and none of them import the notifier, so a maintenance run
announcing the back catalogue to Discord is structurally impossible rather than merely
guarded against.

Every script takes the same flags:

| flag | default | meaning |
|---|---|---|
| `-config` | `config.toml` | the bot's TOML config — the database credentials come from here, so there is no second copy to keep in step |
| `-dry-run` | `false` | report what would change, write nothing |
| `-timeout` | `30m` | overall deadline for the run |

## Order

`rekey-songs` populates the keys the other three depend on. Run it first, and again
after any change to the normalization rules in `utils/matchkey.go`.

```
rekey-songs  →  backfill-stmpd  →  link-remix-parents  →  verify-catalogue
                import-beatport  ↗
```

`verify-catalogue` writes nothing and can be run at any time. Run it last: it names
the pass that repairs whatever it finds, so a non-empty report is a to-do list.

## The scripts

### `rekey-songs`

Recomputes `match_key` and `base_key` for every row. These identify a recording
(song + rendition + artist set) and a song (irrespective of rendition). They are
derived in Go, so a migration cannot fill them in.

- **Requires:** migration `000011_add_match_keys`
- **Idempotent:** yes — rows whose keys already match are not written
- **Writes:** `match_key`, `base_key`

### `backfill-stmpd`

Walks the full STMPD RCRDS catalogue (~1015 releases, read from the label's publicly
readable Sanity dataset) and reconciles it against the table.

- fills in streaming links, artwork and the STMPD slug on rows that already exist
- corrects `release_date` from a `<year>-01-01` placeholder to the exact date, but
  **only** on an exact-identity match — a fuzzy match is grounds to add links, never
  to rewrite a date
- merges a row into its twin when a date correction collides with `unique_release`,
  because that collision means both rows are the same song from two sources
- inserts catalogue releases the table has never held, stamped as already announced

- **Requires:** migrations through `000011`, and `rekey-songs`
- **Idempotent:** yes — a second run reports `rows_written=0`
- **Writes:** all link columns, `thumbnail_url` (only when empty), `stmpd_slug`,
  `release_date`, `stmpd_synced_at`; inserts and deletes rows
- **Riskiest script here.** Take a dump first and read the `-dry-run` output.

It logs a per-tier breakdown of how matches were resolved. Tiers marked `exact=true`
rest on a stable identifier; the rest are inference and only ever add links.

### `link-remix-parents`

Points every remix row at the canonical row for the same song, and flags
instrumentals.

Beatport lists each remix as its own track, so one song becomes many rows —
"Catharina" is six, "Told You So" is twelve. No row is deleted: each is a real
recording with its own beatport id, BPM and length. What changes is presentation.
`parent_song_id IS NULL` is what `/links` autocomplete, `/lyrics`, `/quiz` and the
radio rotation filter on, so the catalogue reads as one entry per song again.

Grouping is by title plus artist *containment*, not `base_key`: beatport credits a
remix to the original artist plus the remixer, so their artist sets differ by
construction and a `base_key` match would never fire.

- **Requires:** migration `000012_remix_parent`, and `rekey-songs`
- **Idempotent:** yes
- **Writes:** `parent_song_id`, `is_instrumental`

### `backfill-dates`

Replaces the `1970-01-01` placeholder release dates with real ones.

The placeholder is what the original importer wrote when it had no date, and it is
not cosmetic: `release_date` drives the announcement recency window, the "released"
footer on a track card, and any date ordering, so a row stuck at 1970 sorts to the
bottom of the catalogue forever.

Dates come from Apple's public lookup API, resolved from the numeric id already
embedded in each row's own `apple_music_url` — not from a search. The date therefore
belongs to the exact recording the row already links to, and no fuzzy matching is
involved. Rows with no Apple link are reported, never guessed at.

Paced at one request every 3 seconds, so a full pass takes a few minutes.

- **Requires:** nothing beyond the base schema
- **Idempotent:** yes — a second run resolves 0
- **Writes:** `release_date`; merges and deletes a row when the corrected date
  collides with an existing twin

Its summary distinguishes three kinds of unresolvable row: no Apple link at all, an
Apple link pointing at a *playlist* rather than a release (a data problem worth
fixing — those buttons send users to the wrong place), and a link Apple no longer
has a record for.

### `dedupe-songs`

Folds together rows that represent the same recording, identified by an exact
`match_key` (artist set + base title + rendition).

They exist for two reasons: the two sources disagree about where a rendition belongs
— STMPD publishes "Grove (Rework)" as a title while beatport files "Grove" with
`mix_name` "Rework" — and for months the old matcher could not pair rows across
sources at all, so the beatport importer inserted its own copy of songs already
present. The symptom is one song offering two identical-looking `/links` entries.

**Not every duplicate is safe to merge blind.** Where two rows disagree on the release
date, one date is about to be discarded, and a wide gap can mean a genuine re-release.
Merging is therefore bounded by `-max-date-gap` (default 120 days, which covers the
normal disagreement between beatport's publish date and STMPD's release date);
anything wider is logged for a human and left untouched.

The surviving row is chosen by provenance: an STMPD slug first, then hand-entered
lyrics (they exist nowhere else and must not be what disappears), then streaming
links, then the earliest real date — a `1970-01-01` placeholder never wins on date.

- **Requires:** `rekey-songs`
- **Idempotent:** yes
- **Writes:** merges and deletes rows; repoints any remixes onto the surviving row

`-report-suspects=out.csv` writes the duplicates an exact `match_key` *cannot* catch
and exits without changing anything. These are rows that agree on the title but
disagree on who made it, in three shapes needing different judgement:

| reason | example | note |
|---|---|---|
| `feature-omitted` | `Moksi Ft. Adam McInnis` vs `Moksi` | one side drops the featured artist |
| `artist-alias-or-corrupt` | `Duncan` vs `Düncan Musique` | same act, two spellings |
| `artist-alias-or-corrupt` | `Able HeartDetonate`, `rionosremixes` | the title bled into the artists field — fix the row, do not merge |

A shared title plus a subset credit is *not* proof of a duplicate, which is why this
reports rather than merges.

### `import-beatport`

One-off unbounded import of the Beatport catalogue — the bot's former
`--fetch-all-beatport` flag. Inserts tracks the table does not already represent,
stamped as already announced.

- **Requires:** working Beatport credentials in the config, and `rekey-songs`
- **Idempotent:** yes — existing tracks resolve through the matcher and are skipped
- **Writes:** inserts rows with beatport metadata; fills `beatport_slug` on rows it
  already has
- Run `link-remix-parents` afterwards so the new remix rows are grouped.

It is also how Beatport links get fixed. A track page is `/track/<slug>/<id>` and the
catalogue only ever stored the id, so every Beatport button led to a 404. The slug
cannot be derived locally — Beatport slugifies the track's full name including the
feature credit that this catalogue strips into the artist column, so "X's feat. Icona
Pop" becomes `xs-feat-icona-pop` where the stored title would give `xs`. This walk has
the authoritative value in hand and stores it on rows it skips as well as rows it
inserts.

### `verify-catalogue`

Read-only. Recomputes each row's derived state and reports every row that disagrees
with what is stored, grouped by invariant, naming the pass that fixes it.

- **Requires:** nothing; safe to run against production at any time
- **Writes:** nothing — it does not import a query that mutates
- **Exit:** always 0; read the `checks_failed` and `rows_flagged` summary

Checks: the collection flag against a recomputation, stale `match_key`/`base_key`,
rows sharing a match key (the same recording twice), rendition trees that are deeper
than one level or rooted at a release or at a row's own id, renditions left unfiled
while their song sits in the table, Beatport ids with no slug to build a URL from,
tracking parameters on streaming links, and songs carrying no link at all.

It exists because the alternative was reading the table by hand. Every defect found
that way turned out to be a class rather than a row — one wrongly flagged collection
was hiding twenty-seven songs from search — so the useful unit of work is the
invariant.

### `backfill-modlogs`

Imports historical moderation from Discord's audit log into `modlogs`, so the
dashboard's moderation page is not empty on the day the audit-log listener ships.

- **Requires:** the bot's role has **View Audit Log** in the guild; `-guild=<id>`
- **Writes:** `modlogs` rows carrying `audit_log_id`
- **Exit:** 0 on success; the summary reports `inserted` and `entries_seen`

```
go run ./scripts/backfill-modlogs -config=config.toml -guild=690950056202731521 -dry-run
go run ./scripts/backfill-modlogs -config=config.toml -guild=690950056202731521
```

Discord retains roughly 45 days of audit history, so this is a one-shot catch-up
rather than a full archive; everything after it is captured live by the listener.
Safe to re-run — rows are keyed on the audit entry ID and inserted with
`ON CONFLICT DO NOTHING`, so a second pass over the same window writes nothing.

Entries whose executor is the bot itself are skipped: the `/moderation` command
that performed them already wrote a row naming the human moderator, and importing
the audit entry too would double-count the action and attribute it to the bot.

## Running against production

The bot on `limitless` runs from `~/STMPDBot` against the shared
`postgres-db-1` container. The scripts are not in the deployed image (the Dockerfile
builds only the bot binary), and there is no Go toolchain on the server — so run them
from a local checkout. Postgres is published on the host's Tailscale address, so no
tunnel is needed:

```toml
# config.prod.toml — gitignore this, it holds the database password
[database]
host = "100.74.136.119"
user = "postgres"
password = "<the password from ~/STMPDBot/config.docker.toml>"
name = "garrixbot"
port = 5432
```

`import-beatport` additionally needs the `[bot]` beatport credentials from that same
file. The other three scripts only read `[database]`.

**Take a dump first.** `backfill-stmpd` deletes merged rows, and there is no undo:

```sh
ssh milind@100.74.136.119 \
  'docker exec postgres-db-1 pg_dump -U postgres -d garrixbot --no-owner' \
  > garrixbot-$(date +%F).sql
```

Then, in order:

```sh
go run ./scripts/rekey-songs        -config=config.prod.toml
go run ./scripts/backfill-stmpd     -config=config.prod.toml -dry-run   # read this
go run ./scripts/backfill-stmpd     -config=config.prod.toml
go run ./scripts/link-remix-parents -config=config.prod.toml
```

Run `backfill-stmpd` until it reports `rows_written=0`. It converges in three or four
passes: each pass records which release owns which row, and a release displaced
earlier in a pass reclaims its row on the next one. Against the August 2026 catalogue
that sequence was 862 → 84 → 4 → 0.

Migrations run automatically when the bot boots, so deploy the new image before
running any of this — `rekey-songs` needs the columns from `000011` to exist.

## Verifying afterwards

```sql
-- Nothing can be announced: every row is stamped.
SELECT count(*) FROM songs WHERE announced_at IS NULL;              -- expect 0

-- Songs still carrying no streaming links, by source.
SELECT source, count(*) FROM songs
WHERE spotify_url IS NULL AND apple_music_url IS NULL AND youtube_url IS NULL
GROUP BY 1;

-- The catalogue as users see it.
SELECT count(*) FILTER (WHERE parent_song_id IS NULL) AS canonical,
       count(*) FILTER (WHERE parent_song_id IS NOT NULL) AS remixes FROM songs;
```
