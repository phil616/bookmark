package app

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type authTransport struct{ key string }

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.key)
	return http.DefaultTransport.RoundTrip(r)
}
func TestMCP(t *testing.T) {
	a := testApp(t)
	s := httptest.NewServer(a.Handler())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{"test-api-key-123456789"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil || len(tools.Tools) != 12 {
		t.Fatal(tools, e)
	}
	resource, e := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bookmarks://analysis-spec/v1"})
	if e != nil || len(resource.Contents) == 0 {
		t.Fatal(resource, e)
	}
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "bookmarks_create", Arguments: map[string]any{"raw": "MCP test", "description": "test", "tags": []string{}, "score": .7}})
	if e != nil || result.IsError {
		t.Fatal(result, e)
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "bookmarks_list", Arguments: map[string]any{}})
	if e != nil || result.IsError {
		t.Fatal(result, e)
	}
}
