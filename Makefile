VERSION := $(shell git describe --tags --always --dirty)

build:
	go build -ldflags "-s -w -X github.com/vinegod/discordgamebridge/internal/version.Version=$(VERSION)" -o bin/discord-gamebridge .

run:
	go run .

test:
	go test ./...

# integration-test runs tests that require real external resources (tmux, scripts, RCON).
# For RCON tests set: RCON_HOST, RCON_PORT, RCON_PASSWORD (and optionally RCON_COMMAND).
integration-test:
	go test -tags integration -v -timeout 60s ./test/integration/...

lint:
	golangci-lint run

pre-commit:
	pre-commit run --all-files

.PHONY: build run test integration-test lint
