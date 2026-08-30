BINARY := engram
BIN_DIR := bin
# Install prefix; the binary lands in $(PREFIX)/bin. Override with `make install PREFIX=/usr/local`.
PREFIX ?= $(HOME)/.local

# Stamp the version from git so `engram version` reports something meaningful
# instead of the 0.0.0-dev default. Falls back to "dev" outside a git checkout.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/davisbuilds/engram/internal/version.Version=$(VERSION)

.PHONY: build install test lint fmt vet clean tag-patch tag-minor tag-major next-version

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/engram

# Build with the version stamp and install onto PATH (default ~/.local/bin).
install:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/engram
	mkdir -p $(PREFIX)/bin
	install -m 0755 $(BIN_DIR)/$(BINARY) $(PREFIX)/bin/$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

fmt:
	gofumpt -w .

clean:
	rm -rf $(BIN_DIR)

# Release tagging: bump the latest vX.Y.Z tag and create an annotated tag locally.
# Pushing is a separate, explicit step (`git push origin <tag>`). `next-version`
# prints what the next patch tag would be without creating anything.
tag-patch:
	scripts/semver-tag.sh patch
tag-minor:
	scripts/semver-tag.sh minor
tag-major:
	scripts/semver-tag.sh major
next-version:
	@scripts/semver-tag.sh patch --print
