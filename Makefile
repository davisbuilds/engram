BINARY := engram
BIN_DIR := bin
# Install prefix; the binary lands in $(PREFIX)/bin. Override with `make install PREFIX=/usr/local`.
PREFIX ?= $(HOME)/.local

# Stamp the version from git so `engram version` reports something meaningful
# instead of the 0.0.0-dev default. Falls back to "dev" outside a git checkout.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/davisbuilds/engram/internal/version.Version=$(VERSION)

.PHONY: build install test test-scripts lint fmt vet clean next-version

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/engram

# Build with the version stamp and install onto PATH (default ~/.local/bin).
install:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/engram
	mkdir -p $(PREFIX)/bin
	install -m 0755 $(BIN_DIR)/$(BINARY) $(PREFIX)/bin/$(BINARY)

test:
	go test ./...

test-scripts:
	shellcheck scripts/*.sh
	scripts/semver-next_test.sh
	scripts/check-pr-title_test.sh
	python3 -m unittest discover -s scripts -p 'test_release_history.py'

vet:
	go vet ./...

lint:
	golangci-lint run

fmt:
	gofumpt -w .

clean:
	rm -rf $(BIN_DIR)

# Read-only diagnostic; Release Please owns release version selection and tags.
# Override LEVEL=minor or LEVEL=major to preview a different mathematical bump.
LEVEL ?= patch
next-version:
	@scripts/semver-next.sh $(LEVEL)
