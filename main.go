// Command vault-search is a thin CLI over a running obsidian-hybrid-search
// Streamable HTTP MCP daemon. It exists so many concurrent agents (of any
// framework, not just MCP-native ones) can query one shared search/index
// process instead of each cold-starting the embedding model. The daemon owns
// the index and freshness; this binary just forwards one tool call and prints.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eitanpo/vault-search/internal/mcpc"
)

// version holds the last published release; it is canonical here. `make build`
// appends a build-timestamp (+.dirty) segment for local builds, and GoReleaser
// overrides it from the git tag on release, both via -ldflags "-X main.version".
var version = "0.1.1"

const defaultURL = "http://127.0.0.1:3939/mcp"

// Exit codes form a contract: 4 (unreachable) must never be confused with
// 0-with-no-results, so a dead daemon is not read as "not in corpus".
const (
	exitOK        = 0
	exitUsage     = 2
	exitUnreach   = 4
	exitToolError = 5
)

func main() { os.Exit(run(os.Args[1:])) }

func run(argv []string) int {
	if len(argv) == 0 {
		usage()
		return exitUsage
	}
	switch argv[0] {
	case "-h", "--help", "help":
		usage()
		return exitOK
	case "--version", "version":
		fmt.Println(version)
		return exitOK
	case "search":
		return cmdSearch(argv[1:])
	case "read":
		return cmdRead(argv[1:])
	case "status":
		return cmdStatus(argv[1:])
	default:
		// Implicit search: `vault-search "query" [flags]`.
		return cmdSearch(argv)
	}
}

// parseInterspersed parses fs allowing flags to appear before, after, or among
// positional arguments. Go's flag package stops at the first non-flag token, so
// `vault-search "query" --limit 15` (query-first, matching upstream ergonomics)
// would otherwise leave --limit unparsed.
func parseInterspersed(fs *flag.FlagSet, argv []string) ([]string, error) {
	var positionals []string
	args := argv
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, args[0])
		args = args[1:]
	}
}

// stringSlice is a repeatable string flag (--tag a --tag b).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// common registers flags shared by every subcommand and returns accessors.
func common(fs *flag.FlagSet) (url *string, timeout *time.Duration, jsonOut *bool) {
	url = fs.String("url", envOr("VAULT_SEARCH_URL", defaultURL), "daemon MCP endpoint")
	timeout = fs.Duration("timeout", envDurationOr("VAULT_SEARCH_TIMEOUT", 10*time.Second), "per-call deadline")
	// Default to JSON when stdout is not a TTY so piped consumers (agents) get
	// structured output without passing --json; humans at a terminal get tables.
	jsonOut = fs.Bool("json", !stdoutIsTTY(), "output JSON (default when stdout is not a TTY)")
	return
}

func cmdSearch(argv []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	url, timeout, jsonOut := common(fs)
	mode := fs.String("mode", "hybrid", "search mode: hybrid|semantic|fulltext|title")
	limit := fs.Int("limit", 10, "maximum results")
	threshold := fs.Float64("threshold", 0, "minimum score 0..1")
	var tags, scopes stringSlice
	fs.Var(&tags, "tag", "filter by tag (repeatable; prefix - to exclude)")
	fs.Var(&scopes, "scope", "limit to subfolder (repeatable; prefix - to exclude)")
	queries, err := parseInterspersed(fs, argv)
	if err != nil {
		return exitUsage
	}

	if len(queries) == 0 && len(tags) == 0 && len(scopes) == 0 {
		fmt.Fprintln(os.Stderr, "vault-search: search needs a query or a --tag/--scope filter")
		return exitUsage
	}

	args := buildSearchArgs(queries, *mode, *limit, *threshold, tags, scopes)
	return call(*url, *timeout, "search", args, *jsonOut)
}

// buildSearchArgs maps CLI inputs to the obsidian-hybrid-search `search` tool's
// argument keys. These keys are confirmed against the live tools/list schema and
// pinned by TestBuildSearchArgs — do not rename without updating both.
func buildSearchArgs(queries []string, mode string, limit int, threshold float64, tags, scopes []string) map[string]any {
	args := map[string]any{"mode": mode, "limit": limit}
	switch len(queries) {
	case 0:
		// filter-only search
	case 1:
		args["query"] = queries[0]
	default:
		args["queries"] = queries
	}
	if threshold > 0 {
		args["threshold"] = threshold
	}
	if len(tags) > 0 {
		args["tag"] = []string(tags)
	}
	if len(scopes) > 0 {
		args["scope"] = []string(scopes)
	}
	return args
}

func cmdRead(argv []string) int {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	url, timeout, jsonOut := common(fs)
	snippet := fs.Int("snippet-length", 0, "max content length per note (0 = full)")
	paths, err := parseInterspersed(fs, argv)
	if err != nil {
		return exitUsage
	}
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "vault-search: read needs at least one note path")
		return exitUsage
	}
	args := buildReadArgs(paths, *snippet)
	return call(*url, *timeout, "read", args, *jsonOut)
}

// buildReadArgs maps note paths to the `read` tool's arguments. Its required arg
// is `paths` (plural), accepting a single string or an array — confirmed against
// the live tools/list schema and pinned by TestBuildReadArgs.
func buildReadArgs(paths []string, snippet int) map[string]any {
	args := map[string]any{}
	if len(paths) == 1 {
		args["paths"] = paths[0]
	} else {
		args["paths"] = paths
	}
	if snippet > 0 {
		args["snippet_length"] = snippet
	}
	return args
}

func cmdStatus(argv []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	url, timeout, jsonOut := common(fs)
	if _, err := parseInterspersed(fs, argv); err != nil {
		return exitUsage
	}
	return call(*url, *timeout, "status", map[string]any{}, *jsonOut)
}

// call performs the tool invocation and prints the result, returning the
// process exit code.
func call(url string, timeout time.Duration, tool string, args map[string]any, jsonOut bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := &mcpc.Client{Endpoint: url, Version: version}
	res, err := c.Call(ctx, tool, args)
	if err != nil {
		if errors.Is(err, mcpc.ErrUnreachable) {
			fmt.Fprintf(os.Stderr, "vault-search: no daemon at %s — start it with 'obsidian-hybrid-search serve'\n", url)
			return exitUnreach
		}
		fmt.Fprintf(os.Stderr, "vault-search: %v\n", err)
		return exitToolError
	}
	if res.IsError {
		// Tool-level error: surface its text on stderr.
		fmt.Fprintln(os.Stderr, "vault-search: "+strings.Join(res.Text, "\n"))
		return exitToolError
	}
	output(res, jsonOut, tool)
	return exitOK
}

// output prints a result. The daemon returns its payload as a JSON string in
// TextContent (StructuredContent is empty), so we parse that. Human search
// results render as a rank/score/path table; everything else prints as JSON.
func output(res *mcpc.Result, jsonOut bool, tool string) {
	p := payload(res)
	if !jsonOut && tool == "search" {
		if table, ok := renderSearchTable(p); ok {
			fmt.Print(table)
			return
		}
	}
	if p != nil {
		if b, err := json.MarshalIndent(p, "", "  "); err == nil {
			fmt.Println(string(b))
			return
		}
	}
	if len(res.Text) > 0 {
		fmt.Println(strings.Join(res.Text, "\n"))
	}
}

// payload returns the tool's structured result: StructuredContent if the server
// set it, otherwise the TextContent parsed as JSON (how this daemon replies).
func payload(res *mcpc.Result) any {
	if res.Structured != nil {
		return res.Structured
	}
	var v any
	if err := json.Unmarshal([]byte(strings.Join(res.Text, "\n")), &v); err == nil {
		return v
	}
	return nil
}

// renderSearchTable renders the search tool's {results:[...]} payload as a
// compact human table. The keys (results, rank, score, path, snippet) are the
// live search-tool schema, confirmed against the daemon's tools/list.
func renderSearchTable(structured any) (string, bool) {
	m, ok := structured.(map[string]any)
	if !ok {
		return "", false
	}
	results, ok := m["results"].([]any)
	if !ok {
		return "", false
	}
	if len(results) == 0 {
		return "no results\n", true
	}
	var b strings.Builder
	for _, r := range results {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		rank := numOr(row["rank"], 0)
		score := numOr(row["score"], 0)
		path, _ := row["path"].(string)
		fmt.Fprintf(&b, "%2d  %.3f  %s\n", int(rank), score, path)
		if snip, _ := row["snippet"].(string); snip != "" {
			fmt.Fprintf(&b, "      %s\n", oneLine(snip, 120))
		}
	}
	return b.String(), true
}

func numOr(v any, def float64) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return def
}

// oneLine collapses whitespace and truncates to max runes for a table cell.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDurationOr(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func usage() {
	fmt.Fprint(os.Stderr, `vault-search — thin CLI over an obsidian-hybrid-search serve daemon

Usage:
  vault-search <query>... [flags]      search (default subcommand)
  vault-search search <query>... [flags]
  vault-search read <path>... [flags]
  vault-search status [flags]

Global flags:
  --url <url>        daemon endpoint (env VAULT_SEARCH_URL; default `+defaultURL+`)
  --timeout <dur>    per-call deadline (env VAULT_SEARCH_TIMEOUT; default 10s)
  --json             output JSON (default when stdout is not a TTY)

Search flags:
  --mode <m>         hybrid|semantic|fulltext|title (default hybrid)
  --limit <n>        max results (default 10)
  --threshold <n>    min score 0..1
  --tag <t>          filter by tag (repeatable; prefix - to exclude)
  --scope <f>        limit to subfolder (repeatable; prefix - to exclude)

Exit codes: 0 ok · 2 usage · 4 daemon unreachable · 5 tool error
`)
}
