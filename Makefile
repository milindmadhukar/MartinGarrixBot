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
	go build -o garrixbot cmd/main.go

dev:
	air -c .air.toml

run:
	go run . --sync-commands

kill:
	@pgrep -f "mgbot_bin\|garrixbot" | xargs kill || true