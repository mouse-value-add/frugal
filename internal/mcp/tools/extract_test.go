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

	"github.com/frugalsh/frugal/internal/extract"
	"github.com/frugalsh/frugal/internal/routing"
)

func decodeExtractOutput(raw any) (ExtractOutput, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return ExtractOutput{}, err
	}
	var out ExtractOutput
	if err := json.Unmarshal(b, &out); err != nil {
		return ExtractOutput{}, err
	}
	return out, nil
}

type fakeExtractor struct {
	name      string
	cost      float64
	res       extract.Result
	err       error
	lastQuery extract.Query
}

func (f *fakeExtractor) Name() string         { return f.name }
func (f *fakeExtractor) CostPerCall() float64 { return f.cost }
func (f *fakeExtractor) Extract(_ context.Context, q extract.Query) (extract.Result, error) {
	f.lastQuery = q
	if f.err != nil {
		return extract.Result{}, f.err
	}
	return f.res, nil
}

func newExtractServer() *sdkmcp.Server {
	return sdkmcp.NewServer(&sdkmcp.Implementation{Name: "frugal-test", Version: "test"}, &sdkmcp.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func dialExtractClient(t *testing.T, srv *sdkmcp.Server) (*sdkmcp.ClientSession, func()) {
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

func TestRegisterExtract_ToolAppearsInToolsList(t *testing.T) {
	srv := newExtractServer()
	RegisterExtract(srv, []extract.Extractor{
		&fakeExtractor{name: "fake", cost: 0},
	}, nil)
	client, cleanup := dialExtractClient(t, srv)
	defer cleanup()
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "frugal__extract" {
		t.Errorf("expected frugal__extract; got %+v", res.Tools)
	}
}

func TestRegisterExtract_NoExtractorsSkipsRegistration(t *testing.T) {
	srv := newExtractServer()
	RegisterExtract(srv, nil, nil)
	client, cleanup := dialExtractClient(t, srv)
	defer cleanup()
	res, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 0 {
		t.Errorf("expected 0 tools when no extractors configured; got %d", len(res.Tools))
	}
}

func TestCallTool_ExtractRoutesToCheapest(t *testing.T) {
	free := &fakeExtractor{
		name: "free", cost: 0,
		res: extract.Result{Markdown: "FREE", Title: "T", CostUSD: 0},
	}
	paid := &fakeExtractor{
		name: "paid", cost: 0.001,
		res: extract.Result{Markdown: "should-not-be-used", CostUSD: 0.001},
	}
	srv := newExtractServer()
	RegisterExtract(srv, []extract.Extractor{paid, free}, nil)
	client, cleanup := dialExtractClient(t, srv)
	defer cleanup()
	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "frugal__extract",
		Arguments: map[string]any{"url": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned isError: %+v", res.Content)
	}
	out, err := decodeExtractOutput(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ProviderUsed != "free" {
		t.Errorf("provider_used: got %q want free", out.ProviderUsed)
	}
	if out.Markdown != "FREE" {
		t.Errorf("unexpected markdown: %q", out.Markdown)
	}
	if paid.lastQuery.URL != "" {
		t.Errorf("paid should not have been called; lastQuery=%+v", paid.lastQuery)
	}
}

func TestCallTool_ExtractFallsBackOnTransient(t *testing.T) {
	free := &fakeExtractor{
		name: "free", cost: 0,
		err: routing.Transient("free", 503, errors.New("blip")),
	}
	paid := &fakeExtractor{
		name: "paid", cost: 0.001,
		res: extract.Result{Markdown: "via-paid", CostUSD: 0.001},
	}
	srv := newExtractServer()
	RegisterExtract(srv, []extract.Extractor{paid, free}, nil)
	client, cleanup := dialExtractClient(t, srv)
	defer cleanup()
	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "frugal__extract",
		Arguments: map[string]any{"url": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	out, err := decodeExtractOutput(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ProviderUsed != "paid" || out.Markdown != "via-paid" {
		t.Errorf("expected fallback to paid; got %+v", out)
	}
}

func TestCallTool_ExtractUnknownProviderErrors(t *testing.T) {
	srv := newExtractServer()
	RegisterExtract(srv, []extract.Extractor{&fakeExtractor{name: "only", cost: 0}}, nil)
	client, cleanup := dialExtractClient(t, srv)
	defer cleanup()
	res, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "frugal__extract",
		Arguments: map[string]any{
			"url":      "https://x",
			"provider": "does-not-exist",
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected isError=true for unknown provider, got %+v", res)
	}
}
