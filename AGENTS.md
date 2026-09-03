This is a purpose-built Discord bot for the STMPD RCRDS community, written in Golang with the help of the Disgo package.
Imporant commands handling migrations, sqlc generation are in the Makefile

Listeners will always go in the listeners/ folder
Commands always go in the commands/ folder

Any utility / resusable code that is not part of the core logic should go in the utils folder

We will use a cache first then rest type approach, always check the Caches() and then only go for Rest() calls on cache misses unless it is always better to do Rest calls when we know it
will almost most certainly be a cache miss most times then

## The song catalogue lives on the VPS

The `songs` table on the **limitless VPS** (`100.74.136.119:5432`, database `garrixbot`)
is the authoritative catalogue. It is the copy the bot announces from, the copy the
dashboard edits, and the only copy that accumulates hand corrections.

- **Local is a copy, never a source.** Refresh the local database *from* the VPS before
  working on anything catalogue-shaped; never push a local `songs` table up.
- **Every catalogue change is made on the VPS first** — a merge, a re-key, a hand
  correction, a maintenance script from `scripts/` — and only then copied down to local.
  Fixing a duplicate locally and letting it "sync later" loses the fix: the next import
  or backfill run against the VPS overwrites nothing locally, and the bug is still live.
- Reach the VPS database from a local checkout with the credentials in `config.prod.toml`.
  There is no local `psql` binary; the local `postgres-db-1` container has one:

  ```
  docker exec -e PGPASSWORD='<config.prod.toml>' postgres-db-1 \
    psql -h 100.74.136.119 -U postgres -d garrixbot
  ```

- The `scripts/` passes take `-config config.prod.toml` for the same reason, and every
  one of them supports `-dry-run`. Run the dry run against the VPS and read it before
  running the real pass.
- To refresh local after a change lands on the VPS — dump there, restore here:

  ```
  docker exec -e PGPASSWORD='<prod>' postgres-db-1 \
    pg_dump -h 100.74.136.119 -U postgres -d garrixbot -Fc -f /tmp/garrixbot.dump
  docker exec -e PGPASSWORD='<local>' postgres-db-1 \
    pg_restore -U postgres -d garrixbot --clean --if-exists --no-owner /tmp/garrixbot.dump
  ```

  Both run in the *local* container; the first reaches the VPS over Tailscale. Compare
  `select count(*) from songs` on each side afterwards — they should be equal, give or
  take rows the bot inserted while the dump was running.
- Never point the integration tests at either database. They apply migrations and write
  rows: `createdb garrixbot_itest` and set `STMPD_TEST_DATABASE_URL` at it instead.
