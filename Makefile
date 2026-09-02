ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

migrate_up:
	docker run -v $(ROOT_DIR)/db/migrations:/migrations --network=host migrate/migrate -path=migrations/ -database postgresql://postgres:password@localhost:5432/garrixbot?sslmode=disable up

migrate_down:
	docker run -v $(ROOT_DIR)/db/migrations:/migrations --network=host migrate/migrate -path=migrations/ -database postgresql://postgres:password@localhost:5432/garrixbot?sslmode=disable down 1

make_migration:
	@read -p "Enter file name: " MIGRATION_NAME; \
	go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate create -ext sql -dir db/migrations -seq $$MIGRATION_NAME

# Pinned: sqlc/sqlc:latest regenerates every file in db/sqlc, so a generator
# version drift shows up as an unrelated whole-directory diff.
sqlc:
	docker run --rm -v $(ROOT_DIR):/src -w /src sqlc/sqlc:1.31.1 generate

psql:
	docker exec -it postgres-db-1 psql -U postgres -d garrixbot

migrate_force:
	@read -p "Enter version to force: " VERSION; \
	docker run -v $(ROOT_DIR)/db/migrations:/migrations --network=host migrate/migrate -path=migrations/ -database postgresql://postgres:password@localhost:5432/garrixbot?sslmode=disable force $$VERSION

build:
	go build -o stmpd-bot .

dev:
	air -c .air.toml

run:
	go run . --sync-commands

kill:
	@pgrep -f "stmpdbot_bin\|stmpd-bot" | xargs kill || true
# --- maintenance scripts (see scripts/README.md) ---------------------------------
# CONFIG defaults to the bot's own config; override for a non-default database:
#   make backfill-stmpd CONFIG=config.docker.toml
CONFIG ?= config.toml

rekey_songs:
	go run ./scripts/rekey-songs -config=$(CONFIG)

backfill_stmpd_dry:
	go run ./scripts/backfill-stmpd -config=$(CONFIG) -dry-run

backfill_stmpd:
	go run ./scripts/backfill-stmpd -config=$(CONFIG)

link_remix_parents:
	go run ./scripts/link-remix-parents -config=$(CONFIG)

import_beatport:
	go run ./scripts/import-beatport -config=$(CONFIG)

backfill_dates_dry:
	go run ./scripts/backfill-dates -config=$(CONFIG) -dry-run

backfill_dates:
	go run ./scripts/backfill-dates -config=$(CONFIG)

dedupe_songs_dry:
	go run ./scripts/dedupe-songs -config=$(CONFIG) -dry-run

dedupe_songs:
	go run ./scripts/dedupe-songs -config=$(CONFIG)

backfill_lyrics_dry:
	go run ./scripts/backfill-lyrics -config=$(CONFIG) -dry-run

# Paced at LRCLIB's requested 500ms between requests, so a full sweep takes ~20
# minutes -- past the 30m default only if the backlog grows a lot.
backfill_lyrics:
	go run ./scripts/backfill-lyrics -config=$(CONFIG) -timeout=60m

verify_catalogue:
	go run ./scripts/verify-catalogue -config=$(CONFIG)

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

vet:
	go vet ./...

test:
	go test -race -shuffle=on ./...

test-integration:
	go test -tags integration -count=1 ./db/...

cover:
	go test -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

cover-html: cover
	go tool cover -html=coverage.out -o coverage.html

fuzz:
	go test -run=^$$ -fuzz=Fuzz -fuzztime=30s ./stmpdbot/handlers/

# Hits the real tour and STMPD pages to check they still serve the shape the
# parsers expect. Not part of `test` or CI; run it when a fetcher goes quiet.
live-check:
	go test -tags livefetch -count=1 -v ./stmpdbot/handlers/

check: fmt-check vet test

.PHONY: build dev run kill fmt fmt-check vet test test-integration cover cover-html fuzz live-check check \
	migrate_up migrate_down make_migration sqlc psql migrate_force

lint:
	golangci-lint run

# --- dashboard -------------------------------------------------------------
# The standalone Tailwind CLI is a single static binary with no Node runtime.
# Its output, dashboard/static/app.css, is COMMITTED and go:embed-ed, which is
# what keeps Node out of the Docker image entirely.
TAILWIND ?= ./bin/tailwindcss

tailwind_install:
	mkdir -p bin
	curl -sL -o $(TAILWIND) https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64
	chmod +x $(TAILWIND)

tailwind:
	$(TAILWIND) -i dashboard/tailwind.css -o dashboard/static/app.css --minify

tailwind_watch:
	$(TAILWIND) -i dashboard/tailwind.css -o dashboard/static/app.css --watch

# -dev re-parses templates from disk per request, so edits show up on reload.
dashboard:
	go run ./cmd/dashboard -config=$(CONFIG) -dev

build_dashboard:
	go build -o stmpddashboard ./cmd/dashboard
