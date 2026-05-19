package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServer_InitializeAndEmptyToolsList drives a real MCP client against
// the scaffold over the SDK's in-memory transport. This is the round-trip
// the scaffold has to pass before we register any tools — proves
// initialize works and tools/list returns an empty list cleanly.
func TestServer_InitializeAndEmptyToolsList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv := New("frugal", "test-version", slog.New(slog.NewTextHandler(io.Discard, nil)))

	clientT, serverT := sdkmcp.NewInMemoryTransports()

	// Server side: connect first so the client's initialize handshake has
	// something to talk to.
	serverSession, err := srv.Inner.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	// Client side: connect runs the initialize handshake automatically.
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	// tools/list should succeed with zero entries — scaffold has no tools
	// registered yet.
	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if got := len(result.Tools); got != 0 {
		names := make([]string, 0, got)
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		t.Errorf("expected 0 tools registered on scaffold, got %d (%v)", got, names)
	}
}

// TestServer_HTTPHandlerRespondsToInitialize confirms the Streamable HTTP
// transport answers a hand-crafted initialize request. Stops short of a
// full session — just proves the handler is wired and returns a 200 with a
// JSON-RPC body — because the in-memory transport already covers the full
// session lifecycle and StreamableHTTPHandler integration tests belong in
// the SDK itself, not here.
func TestServer_ServeHTTPRejectsEmptyAddress(t *testing.T) {
	srv := New("frugal", "test-version", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := srv.ServeHTTP(ctx, "   ")
	if err == nil || !strings.Contains(err.Error(), "empty listen address") {
		t.Fatalf("expected empty listen address error, got %v", err)
	}
}

func TestServer_HTTPHandlerRespondsToInitialize(t *testing.T) {
	srv := New("frugal", "test-version", slog.New(slog.NewTextHandler(io.Discard, nil)))

	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return srv.Inner
	}, nil)
	testSrv := httptest.NewServer(handler)
	defer testSrv.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"test"}}}`
	req, err := http.NewRequest(http.MethodPost, testSrv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}
}
