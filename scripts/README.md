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
rekey-songs  →  backfill-stmpd  →  link-remix-parents
                import-beatport  ↗
```

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

### `import-beatport`

One-off unbounded import of the Beatport catalogue — the bot's former
`--fetch-all-beatport` flag. Inserts tracks the table does not already represent,
stamped as already announced.

- **Requires:** working Beatport credentials in the config, and `rekey-songs`
- **Idempotent:** yes — existing tracks resolve through the matcher and are skipped
- **Writes:** inserts rows with beatport metadata
- Run `link-remix-parents` afterwards so the new remix rows are grouped.

## Running against production

The bot on `limitless` runs from `~/MartinGarrixBot` against the shared
`postgres-db-1` container. The scripts are not in the deployed image (the Dockerfile
builds only the bot binary), and there is no Go toolchain on the server — so run them
from a local checkout. Postgres is published on the host's Tailscale address, so no
tunnel is needed:

```toml
# config.prod.toml — gitignore this, it holds the database password
[database]
host = "100.74.136.119"
user = "postgres"
password = "<the password from ~/MartinGarrixBot/config.docker.toml>"
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
