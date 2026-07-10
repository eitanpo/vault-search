# vault-search — Product

## Why

The research vault is searched by many agents, often concurrently. The obvious approach — each agent shelling out to `obsidian-hybrid-search search` — breaks under concurrency: every one-shot invocation cold-starts the embedding model, so N concurrent calls load N copies and exhaust memory (observed SIGKILL / exit 137).

The fix is to run **one** persistent `obsidian-hybrid-search serve` daemon that holds the model and index, and have agents talk to it. That daemon speaks MCP over Streamable HTTP. Two ways to consume it:

- **Native MCP config** — works only for MCP-native hosts, and MCP configuration does not globalize across frameworks (each host keeps its own registry). Every new agent type is new config.
- **A CLI on PATH** — universally invokable by anything that can spawn a process. Lower marginal cost per new agent type.

`vault-search` is that CLI: a thin client that forwards one tool call to the daemon. It keeps CLI ergonomics while all the heavy state (model, index, freshness) lives in the shared daemon.

## Design decisions

- **Thin and stateless.** One invocation = connect → one `tools/call` → close. No local index, no model, no session reuse. Startup is dominated by process spawn + one loopback round-trip; the daemon does the real work. `DisableStandaloneSSE` is set — a one-shot client never needs server-initiated messages.
- **The daemon owns freshness.** Its file watcher keeps the index current. This CLI never reindexes and never checks staleness — that logic does not belong in a client.
- **Exit-code contract.** `0` ok, `2` usage, `4` daemon unreachable, `5` tool error. `4` is deliberately distinct: a dead daemon must never be mistaken for an empty result set by a caller (e.g. a recall skill treating it as "not in corpus").
- **Argument keys are the daemon's, confirmed live.** The `search`/`read` tool argument names (`query`/`queries`, `paths`, `mode`, `limit`, …) were confirmed against the daemon's `tools/list`, not documentation prose, and are pinned by unit tests. `read`'s required key is `paths` (plural).
- **Output adapts to the consumer.** Human table on a TTY; JSON when piped, so agents capturing stdout get structured data without a flag.

## Scope

In: `search`, `read`, `status` against a running daemon. Out (for now): `reindex` (the watcher covers it), completion scripts, and any index management — those are the daemon's job, not the client's.
