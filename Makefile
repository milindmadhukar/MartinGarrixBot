ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

migrate_up:
	docker run -v $(ROOT_DIR)/db/migrations:/migrations --network=host migrate/migrate -path=migrations/ -database postgresql://postgres:password@localhost:5432/garrixbot?sslmode=disable up

migrate_down:
	docker run -v $(ROOT_DIR)/db/migrations:/migrations --network=host migrate/migrate -path=migrations/ -database postgresql://postgres:password@localhost:5432/garrixbot?sslmode=disable down 1

make_migration:
	@read -p "Enter file name: " MIGRATION_NAME; \
	go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate create -ext sql -dir db/migrations -seq $$MIGRATION_NAME

sqlc:
	docker run --rm -v $(ROOT_DIR):/src -w /src sqlc/sqlc generate

psql:
	docker exec -it postgres-db-1 psql -U postgres -d garrixbot

migrate_force:
	@read -p "Enter version to force: " VERSION; \
	docker run -v $(ROOT_DIR)/db/migrations:/migrations --network=host migrate/migrate -path=migrations/ -database postgresql://postgres:password@localhost:5432/garrixbot?sslmode=disable force $$VERSION

build:
	go build -o garrixbot .

dev:
	air -c .air.toml

run:
	go run . --sync-commands

kill:
	@pgrep -f "mgbot_bin\|garrixbot" | xargs kill || true

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
	go test -run=^$$ -fuzz=Fuzz -fuzztime=30s ./mgbot/handlers/

check: fmt-check vet test

.PHONY: build dev run kill fmt fmt-check vet test test-integration cover cover-html fuzz check \
	migrate_up migrate_down make_migration sqlc psql migrate_force
