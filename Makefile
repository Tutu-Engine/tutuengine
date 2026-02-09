.PHONY: build test lint clean run serve

# Version info injected at build time
VERSION ?= 0.1.0-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Build the tutu binary
build:
	go build $(LDFLAGS) -o bin/tutu.exe ./cmd/tutu

# Run all tests (no -race: requires CGO which modernc.org/sqlite doesn't use)
test:
	go test -count=1 -cover ./...

# Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

# Clean build artifacts
clean:
	rm -rf bin/
	go clean -cache

# Quick run (build + execute)
run: build
	./bin/tutu.exe

# Start the API server
serve: build
	./bin/tutu.exe serve

# Download all dependencies
deps:
	go mod tidy
	go mod download

# Show test coverage in browser
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out
