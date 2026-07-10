package main

import (
	"flag"
	"github.com/eitanpo/vault-search/internal/mcpc"
	"reflect"
	"strings"
	"testing"
)

// TestParseInterspersed pins the drop-in-compat behavior: flags must be honored
// whether they appear before, after, or among positional arguments.
func TestParseInterspersed(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		wantURL   string
		wantPos   []string
		wantLimit int
	}{
		{"flags before positional", []string{"--url", "u", "--limit", "5", "q1"}, "u", []string{"q1"}, 5},
		{"flags after positional", []string{"q1", "--url", "u", "--limit", "5"}, "u", []string{"q1"}, 5},
		{"flags among positionals", []string{"q1", "--url", "u", "q2", "--limit", "5"}, "u", []string{"q1", "q2"}, 5},
		{"positionals only", []string{"q1", "q2"}, "def", []string{"q1", "q2"}, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			url := fs.String("url", "def", "")
			limit := fs.Int("limit", 10, "")
			pos, err := parseInterspersed(fs, tc.argv)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if *url != tc.wantURL {
				t.Errorf("url = %q, want %q", *url, tc.wantURL)
			}
			if *limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", *limit, tc.wantLimit)
			}
			if !reflect.DeepEqual(pos, tc.wantPos) {
				t.Errorf("positionals = %v, want %v", pos, tc.wantPos)
			}
		})
	}
}

// TestRenderSearchTable pins the human table against the live search schema
// (results[].rank/score/path/snippet). Whitespace in snippets is collapsed.
func TestRenderSearchTable(t *testing.T) {
	structured := map[string]any{
		"results": []any{
			map[string]any{"rank": 1.0, "score": 0.936, "path": "02-Wiki/foo.md", "snippet": "# Foo\n\n  bar   baz"},
			map[string]any{"rank": 2.0, "score": 0.915, "path": "10-Insights/bar.md", "snippet": ""},
		},
	}
	out, ok := renderSearchTable(structured)
	if !ok {
		t.Fatal("expected render ok")
	}
	for _, want := range []string{"02-Wiki/foo.md", "0.936", "# Foo bar baz", "10-Insights/bar.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q; got:\n%s", want, out)
		}
	}
	if empty, ok := renderSearchTable(map[string]any{"results": []any{}}); !ok || empty != "no results\n" {
		t.Errorf("empty results = %q, ok=%v", empty, ok)
	}
	if _, ok := renderSearchTable("not a map"); ok {
		t.Error("non-map should not render")
	}
}

// TestPayloadParsesTextJSON pins the observed wire behavior: the daemon returns
// its payload as a JSON string in TextContent, not StructuredContent.
func TestPayloadParsesTextJSON(t *testing.T) {
	res := &mcpc.Result{Text: []string{`{"results":[{"rank":1,"score":0.5,"path":"a.md"}]}`}}
	p := payload(res)
	m, ok := p.(map[string]any)
	if !ok {
		t.Fatalf("payload not a map: %T", p)
	}
	if _, ok := m["results"].([]any); !ok {
		t.Fatalf("results missing: %+v", m)
	}
}

// TestBuildReadArgs pins the read tool contract: the required key is `paths`
// (plural, string for one, array for many). Guards the live-caught path→paths fix.
func TestBuildReadArgs(t *testing.T) {
	single := buildReadArgs([]string{"a.md"}, 0)
	if single["paths"] != "a.md" {
		t.Errorf("single: paths = %v, want \"a.md\"", single["paths"])
	}
	if _, exists := single["path"]; exists {
		t.Error("must not send singular `path`")
	}
	multi := buildReadArgs([]string{"a.md", "b.md"}, 2000)
	if got, ok := multi["paths"].([]string); !ok || len(got) != 2 {
		t.Errorf("multi: paths = %v", multi["paths"])
	}
	if multi["snippet_length"] != 2000 {
		t.Errorf("snippet_length = %v, want 2000", multi["snippet_length"])
	}
}

// TestBuildSearchArgs pins the search tool contract: single query → `query`,
// multiple → `queries`; mode/limit always sent; optional keys only when set.
func TestBuildSearchArgs(t *testing.T) {
	one := buildSearchArgs([]string{"q"}, "hybrid", 10, 0, nil, nil)
	if one["query"] != "q" || one["mode"] != "hybrid" || one["limit"] != 10 {
		t.Errorf("single query args = %+v", one)
	}
	if _, ok := one["queries"]; ok {
		t.Error("single query must not send `queries`")
	}
	if _, ok := one["threshold"]; ok {
		t.Error("threshold 0 must be omitted")
	}
	many := buildSearchArgs([]string{"a", "b"}, "semantic", 5, 0.2, []string{"t"}, []string{"s"})
	if got, ok := many["queries"].([]string); !ok || len(got) != 2 {
		t.Errorf("multi queries = %v", many["queries"])
	}
	if _, ok := many["query"]; ok {
		t.Error("multi query must not send `query`")
	}
	if many["threshold"] != 0.2 {
		t.Errorf("threshold = %v", many["threshold"])
	}
}
