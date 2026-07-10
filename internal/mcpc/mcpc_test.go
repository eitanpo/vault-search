package mcpc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeDaemon stands up the SDK's own Streamable HTTP server in-process with a
// "search" tool that echoes its query back. This exercises a real MCP
// initialize + tools/call exchange over HTTP without the external daemon, the
// embedding model, or a live vault.
func fakeDaemon(t *testing.T) *httptest.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake-ohs", Version: "test"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "search", Description: "fake search"},
		func(_ context.Context, _ *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
			q, _ := in["query"].(string)
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "got:" + q}},
			}, nil, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return httptest.NewServer(handler)
}

func TestCall_SearchRoundTrip(t *testing.T) {
	ts := fakeDaemon(t)
	defer ts.Close()

	c := &Client{Endpoint: ts.URL, Version: "test"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.Call(ctx, "search", map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(res.Text) != 1 || res.Text[0] != "got:hello" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.IsError {
		t.Fatalf("IsError should be false")
	}
}

func TestCall_DaemonDown(t *testing.T) {
	// Port 1 has nothing listening: connect must fail as ErrUnreachable, not as
	// an empty-but-successful result.
	c := &Client{Endpoint: "http://127.0.0.1:1/mcp", Version: "test"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := c.Call(ctx, "search", map[string]any{"query": "x"})
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("expected ErrUnreachable, got %v", err)
	}
}
