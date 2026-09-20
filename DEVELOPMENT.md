# Development

Go module. The MCP client wraps the official [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).

## Prerequisites

- Go (see `go.mod` for the version)
- [GoReleaser](https://goreleaser.com) for releases (`brew install goreleaser`)
- A running `obsidian-hybrid-search serve` daemon for the live smoke test. Check liveness with `lsof -iTCP:3939`, `vault-search status` (it actually connects), or — if launchd-managed — `launchctl print`. Note: `serve status`/`serve stop` track only the tool's own self-daemonized state file, so they report "not running" for a `--foreground` daemon (e.g. one started by launchd) even while it is live; the port is the source of truth.

## Build, run, install

`make` (default `build`) compile-checks the module and installs to `~/go/bin`, so the global `vault-search` reflects the latest work. `make install` is the install step alone (reused by `release`). Plain `go build`/`go install` print the bare base version; `make` appends a build-timestamp `+.dirty` segment so a local build is visibly not a clean release.

## Tests

`make test` (or `go test ./...`). Three layers, each catching a different class:

1. **Input** — flag parsing (`parseInterspersed`: flags honored before/after/among positionals) and argument-key contracts (`buildSearchArgs`/`buildReadArgs`, guarding e.g. `read` sending `paths` not `path`).
2. **Wire** — the client against the SDK's own in-process Streamable HTTP server: round-trip parse, and daemon-down → `ErrUnreachable`.
3. **Live smoke + contract drift** — *not yet automated.* The argument keys and result shape were confirmed manually against a live daemon's `tools/list`. A scheduled check that re-runs `tools/list` and asserts the keys we send still exist is a TODO; it cannot run in ordinary CI (no daemon/vault there).

## Versioning

The base version is canonical in `main.go` (`var version`), holding the **last published** release. `make` appends a UTC build-timestamp build-metadata segment plus `.dirty` for uncommitted trees. GoReleaser sets the full version from the git tag on release via `-ldflags "-X main.version=<v>"`.

Increment policy (pre-1.0): MINOR (`0.x.0`) for backward-compatible feature/behavior additions, PATCH (`0.x.y`) for fixes and refactors; MAJOR reserved for 1.0. `var version` changes **only when releasing**, never in a feature commit.

## Releasing

Releases are cut locally with GoReleaser, driven by [`.goreleaser.yaml`](.goreleaser.yaml). It builds the binaries, creates the GitHub release, and pushes the Homebrew **cask** to `eitanpo/homebrew-tap` (which must exist).

1. Set `var version` in `main.go` to the release version (no `v`, e.g. `0.1.0`); commit.
2. Tag and push: `git tag v0.1.0 && git push origin v0.1.0`.
3. Dry-run: `make release-dry` (builds all targets, publishes nothing).
4. Publish: `make release` (runs `goreleaser release --clean` sourcing both tokens from `gh auth token`, then `make install` so this machine runs what shipped).

macOS binaries are unsigned, so the cask's post-install hook strips the quarantine attribute. Linux has no cask — `go install` instead.
