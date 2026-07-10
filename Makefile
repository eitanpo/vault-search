# Local build/install. The base version is canonical in main.go (var version); here we append
# a UTC build timestamp as the semver build-metadata segment so every local build is distinct,
# plus a ".dirty" marker when the working tree has uncommitted changes — so the global binary's
# --version shows at a glance it is an unreleased dev build, not the clean release.
# Plain `go build`/`go install` (no make) print the bare base version.
BASE    := $(shell sed -n 's/^var version = "\(.*\)"/\1/p' main.go)
DIRTY   := $(shell test -n "$$(git status --porcelain 2>/dev/null)" && echo .dirty)
VERSION := $(BASE)+$(shell date -u +%Y%m%dT%H%M%SZ)$(DIRTY)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# build is the default goal: compile-check the whole module, then install to ~/go/bin so the
# global `vault-search` always reflects the latest work. Running the build IS the install.
.DEFAULT_GOAL := build

.PHONY: build install test release release-dry
build:
	go build ./...
	go install $(LDFLAGS) .
install:
	go install $(LDFLAGS) .
test:
	go test ./...

# Publish a release from the current pushed tag, then refresh this machine's install so it
# runs what was just shipped. Assumes the version tag is created and pushed (see DEVELOPMENT.md).
release:
	GITHUB_TOKEN=$$(gh auth token) HOMEBREW_TAP_GITHUB_TOKEN=$$(gh auth token) goreleaser release --clean
	$(MAKE) install

# Dry run: build all targets locally, publish nothing, install nothing.
release-dry:
	goreleaser release --snapshot --clean
