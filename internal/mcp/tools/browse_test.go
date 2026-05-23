package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frugalsh/frugal/internal/browse"
	"github.com/frugalsh/frugal/internal/routing"
)

func decodeBrowseOutput(raw any) (BrowseOutput, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return BrowseOutput{}, err
	}
	var out BrowseOutput
	if err := json.Unmarshal(b, &out); err != nil {
		return BrowseOutput{}, err
	}
	return out, nil
}

type fakeBrowser struct {
	name      string
	cost      float64
	res       browse.Result
	err       error
	lastQuery browse.Query
}

func (f *fakeBrowser) Name() string         { return f.name }
func (f *fakeBrowser) CostPerCall() float64 { return f.cost }
func (f *fakeBrowser) Render(_ context.Context, q browse.Query) (browse.Result, error) {
	f.lastQuery = q
	if f.err != nil {
		return browse.Result{}, f.err
	}
	return f.res, nil
}

func newBrowseServer() *sdkmcp.Server {
	return sdkmcp.NewServer(&sdkmcp.Implementation{Name: "frugal-test", Version: "test"}, &sdkmcp.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func dialBrowseClient(t *testing.T, srv *sdkmcp.Server) (*sdkmcp.ClientSession, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	clientT, serverT := sdkmcp.NewInMemoryTransports()
	serverSess, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		cancel()
		t.Fatalf("server connect: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "1"}, nil)
	clientSess, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		serverSess.Close()
		cancel()
		t.Fatalf("client connect: %v", err)
	}
	return clientSess, func() {
		clientSess.Close()
		serverSess.Close()
		cancel()
	}
}

func TestRegisterBrowse_ToolAppearsInToolsList(t *testing.T) {
	srv := newBrowseServer()
	RegisterBrowse(srv, []browse.Browser{
		&fakeBrowser{name: "fake", cost: 0.002},
	}, nil)
	client, cleanup := dialBrowseClient(t, srv)
	defer cleanup()
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "frugal__browse" {
		t.Errorf("expected frugal__browse; got %+v", res.Tools)
	}
}

func TestRegisterBrowse_NoBrowsersSkipsRegistration(t *testing.T) {
	srv := newBrowseServer()
	RegisterBrowse(srv, nil, nil)
	client, cleanup := dialBrowseClient(t, srv)
	defer cleanup()
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 0 {
		t.Errorf("expected 0 tools when no browsers configured; got %d", len(res.Tools))
	}
}

func TestCallTool_BrowseHappyPath(t *testing.T) {
	bl := &fakeBrowser{
		name: "browserless", cost: 0.002,
		res: browse.Result{HTML: "<h1>Rendered</h1>", CostUSD: 0.002},
	}
	srv := newBrowseServer()
	RegisterBrowse(srv, []browse.Browser{bl}, nil)
	client, cleanup := dialBrowseClient(t, srv)
	defer cleanup()
	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "frugal__browse",
		Arguments: map[string]any{
			"url":     "https://example.com/spa",
			"wait_ms": 500,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned isError: %+v", res.Content)
	}
	out, err := decodeBrowseOutput(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ProviderUsed != "browserless" {
		t.Errorf("provider_used: got %q", out.ProviderUsed)
	}
	if out.HTML != "<h1>Rendered</h1>" {
		t.Errorf("HTML: got %q", out.HTML)
	}
	if out.CostUSD != 0.002 {
		t.Errorf("CostUSD: got %v want 0.002", out.CostUSD)
	}
	if bl.lastQuery.WaitForMS != 500 {
		t.Errorf("WaitForMS not propagated; got %d", bl.lastQuery.WaitForMS)
	}
}

func TestCallTool_BrowseFallsBackOnTransient(t *testing.T) {
	first := &fakeBrowser{name: "first", cost: 0.001,
		err: routing.Transient("first", 503, errors.New("blip"))}
	second := &fakeBrowser{name: "second", cost: 0.002,
		res: browse.Result{HTML: "via-second", CostUSD: 0.002}}
	srv := newBrowseServer()
	RegisterBrowse(srv, []browse.Browser{second, first}, nil)
	client, cleanup := dialBrowseClient(t, srv)
	defer cleanup()
	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "frugal__browse",
		Arguments: map[string]any{"url": "https://x"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	out, err := decodeBrowseOutput(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ProviderUsed != "second" || out.HTML != "via-second" {
		t.Errorf("expected fallback to second; got %+v", out)
	}
}

func TestCallTool_BrowseUnknownProviderErrors(t *testing.T) {
	srv := newBrowseServer()
	RegisterBrowse(srv, []browse.Browser{&fakeBrowser{name: "only", cost: 0.002}}, nil)
	client, cleanup := dialBrowseClient(t, srv)
	defer cleanup()
	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "frugal__browse",
		Arguments: map[string]any{
			"url":      "https://x",
			"provider": "does-not-exist",
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected isError=true for unknown provider; got %+v", res)
	}
}

func TestCallTool_BrowseProviderNormalization_TrimAndCaseInsensitive(t *testing.T) {
	only := &fakeBrowser{name: "browserless", cost: 0.002, res: browse.Result{HTML: "ok", CostUSD: 0.002}}
	srv := newBrowseServer()
	RegisterBrowse(srv, []browse.Browser{only}, nil)
	client, cleanup := dialBrowseClient(t, srv)
	defer cleanup()

	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "frugal__browse",
		Arguments: map[string]any{
			"url":      "https://example.com",
			"provider": "  BrOwSeRlEsS  ",
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport: %v", err)
	}
	out, err := decodeBrowseOutput(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ProviderUsed != "browserless" {
		t.Errorf("normalized provider override failed: got %q want browserless", out.ProviderUsed)
	}
}
