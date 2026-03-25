VERSION := $(shell git describe --tags --always --dirty)

build:
	go build -ldflags "-s -w -X github.com/vinegod/discordgamebridge/internal/version.Version=$(VERSION)" -o bin/discord-gamebridge .

run:
	go run .

test:
	go test ./...

lint:
	golangci-lint run

pre-commit:
	pre-commit run --all-files

.PHONY: build run test lint
