// Package mcpc is a thin, stateless client over a running
// obsidian-hybrid-search Streamable HTTP MCP daemon. Each Call performs one
// connect -> tools/call -> close and holds no state between invocations: the
// daemon owns the index, the model, and freshness.
package mcpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrUnreachable means the daemon could not be contacted at the endpoint
// (connection refused or the deadline elapsed before any response). Callers map
// this to a dedicated exit code so a dead daemon is never mistaken for an empty
// result — the distinction the recall skill depends on.
var ErrUnreachable = errors.New("daemon unreachable")

// Client is bound to one daemon endpoint. It is safe to construct per process.
type Client struct {
	Endpoint string // e.g. http://127.0.0.1:3939/mcp
	Version  string // reported to the server as the client implementation version
}

// Result is the normalized outcome of a tool call.
type Result struct {
	Text       []string // TextContent items, in order
	Structured any      // StructuredContent, if the server returned any
	IsError    bool     // the tool reported a tool-level error
}

// Call connects to the daemon, invokes tool with args, and returns the result.
// The session is always closed before returning.
func (c *Client) Call(ctx context.Context, tool string, args map[string]any) (*Result, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "vault-search", Version: c.Version}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: c.Endpoint,
		// One-shot request/response per process — we never need the server to
		// push notifications back, so skip the standalone SSE GET stream. Faster
		// startup, clean teardown.
		DisableStandaloneSSE: true,
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, c.classify(err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, c.classify(err)
	}

	out := &Result{Structured: res.StructuredContent, IsError: res.IsError}
	for _, item := range res.Content {
		if tc, ok := item.(*mcp.TextContent); ok {
			out.Text = append(out.Text, tc.Text)
		}
	}
	return out, nil
}

// classify wraps transport-level failures as ErrUnreachable so callers can
// distinguish "no daemon" from a tool- or protocol-level error.
func (c *Client) classify(err error) error {
	if isUnreachable(err) {
		return fmt.Errorf("%w at %s: %v", ErrUnreachable, c.Endpoint, err)
	}
	return err
}

func isUnreachable(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}
