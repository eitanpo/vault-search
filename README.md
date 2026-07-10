# vault-search

A thin CLI over a running [`obsidian-hybrid-search`](https://github.com/flowing-abyss/obsidian-hybrid-search) `serve` daemon. It forwards one MCP `tools/call` over Streamable HTTP and prints the result — so many concurrent agents (of any framework, not just MCP-native ones) query **one** shared search/index process instead of each cold-starting the embedding model.

See [PRODUCT.md](PRODUCT.md) for why this exists and [DEVELOPMENT.md](DEVELOPMENT.md) for build/release.

## Install

```sh
brew install eitanpo/tap/vault-search   # macOS
go install github.com/eitanpo/vault-search@latest   # any platform
```

Requires a running daemon:

```sh
OBSIDIAN_VAULT_PATH=/path/to/vault obsidian-hybrid-search serve
```

## Usage

```sh
vault-search "hybrid retrieval" --limit 5        # search (default subcommand)
vault-search "task mgmt" "GTD" "todos"           # multi-query fan-out (RRF merge)
vault-search read "02-Wiki/foo.md"               # read note(s)
vault-search status                              # index status / daemon health
```

Output is a human table on a TTY and JSON when piped (override with `--json`).

### Flags

Global: `--url` (env `VAULT_SEARCH_URL`, default `http://127.0.0.1:3939/mcp`), `--timeout` (env `VAULT_SEARCH_TIMEOUT`, default `10s`), `--json`.
Search: `--mode hybrid|semantic|fulltext|title`, `--limit`, `--threshold`, `--tag` (repeatable), `--scope` (repeatable).

Exit codes: `0` ok · `2` usage · `4` daemon unreachable · `5` tool error. Code `4` is distinct so a dead daemon is never mistaken for "no results".

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md). `make` builds and installs; `make test` runs the suite.

## License

MIT — see [LICENSE](LICENSE).
