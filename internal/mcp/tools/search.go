// Package tools registers Frugal's routed MCP tools against an
// *mcp.Server. Each tool here delegates to the relevant internal/provider
// driver(s) via a small interface defined in internal/search (and, in
// later PRs, internal/extract, internal/cache, …).
//
// The tool surface is intentionally narrow: one tool per capability
// (frugal__search, frugal__extract, frugal__chat, …) with the provider
// choice happening inside the handler. Agents see one stable tool name;
// the routing decision is invisible to them.
package tools

import (
	"context"
	"fmt"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frugalsh/frugal/internal/search"
)

// SearchInput is the JSON-schema-generating shape for frugal__search.
// Field tags drive the schema the MCP client sees — keep names + descriptions
// stable across releases, since agents pattern-match on them.
type SearchInput struct {
	Query      string `json:"query" jsonschema:"the search query"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"max results to return (default 5, clamped to 20)"`
	Freshness  string `json:"freshness,omitempty" jsonschema:"optional time window: day | week | month"`
	// Provider pins the search provider for this call ("tavily", "serper", …).
	// When empty or "auto", Frugal picks the cheapest configured provider.
	// Recipe authors use this to override the default for use cases where the
	// eval shows a more expensive provider has materially better recall.
	Provider string `json:"provider,omitempty" jsonschema:"optional provider override: tavily | serper | auto"`
}

// SearchOutput is the structured-content payload returned to the MCP
// client. The result list is what the agent reads to compose its answer;
// CostUSD + ProviderUsed + LatencyMS are the observability footer that
// makes the routing decision auditable in agent traces.
type SearchOutput struct {
	Results      []search.Item `json:"results"`
	CostUSD      float64       `json:"cost_usd"`
	ProviderUsed string        `json:"provider_used"`
	LatencyMS    int64         `json:"latency_ms"`
}

// RegisterSearch wires frugal__search onto the given MCP server. searchers
// is the operator-configured list (one entry per configured provider key).
// A no-op when searchers is empty — the tool is not registered, so
// tools/list won't advertise something the server can't fulfill. That
// distinction matters: agents query tools/list at session start and
// shouldn't see ghost tools that always error.
func RegisterSearch(server *sdkmcp.Server, searchers []search.Searcher) {
	if len(searchers) == 0 {
		return
	}
	desc := fmt.Sprintf(
		"Run a web search routed across %s. Returns a list of {title, url, snippet} hits "+
			"plus the actual provider used and cost paid. Provider choice defaults to the "+
			"cheapest configured; recipe authors can pin via the `provider` argument.",
		joinNames(searchers),
	)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "frugal__search",
		Title:       "Web search (routed)",
		Description: desc,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  false, // search results can shift between calls
			OpenWorldHint:   boolPtr(true),
		},
	}, makeSearchHandler(searchers))
}

func makeSearchHandler(searchers []search.Searcher) func(context.Context, *sdkmcp.CallToolRequest, SearchInput) (*sdkmcp.CallToolResult, SearchOutput, error) {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, in SearchInput) (*sdkmcp.CallToolResult, SearchOutput, error) {
		if in.Query == "" {
			return nil, SearchOutput{}, fmt.Errorf("frugal__search: query is required")
		}
		pick := pickSearcher(searchers, in.Provider)
		if pick == nil {
			if in.Provider != "" && in.Provider != "auto" {
				return nil, SearchOutput{}, fmt.Errorf("frugal__search: provider %q not configured (configured: %s)", in.Provider, joinNames(searchers))
			}
			return nil, SearchOutput{}, fmt.Errorf("frugal__search: no search providers configured")
		}
		start := time.Now()
		res, err := pick.Search(ctx, search.Query{
			Text:       in.Query,
			MaxResults: in.MaxResults,
			Freshness:  in.Freshness,
		})
		latency := time.Since(start).Milliseconds()
		if err != nil {
			return nil, SearchOutput{}, err
		}
		return nil, SearchOutput{
			Results:      res.Items,
			CostUSD:      res.CostUSD,
			ProviderUsed: pick.Name(),
			LatencyMS:    latency,
		}, nil
	}
}

// pickSearcher resolves the per-call provider choice: explicit name wins,
// "auto" / "" falls back to RouteCheapest, unknown explicit name returns
// nil (handler emits a useful "not configured" error).
func pickSearcher(searchers []search.Searcher, requested string) search.Searcher {
	if requested == "" || requested == "auto" {
		return search.RouteCheapest(searchers)
	}
	return search.Find(searchers, requested)
}

func joinNames(searchers []search.Searcher) string {
	if len(searchers) == 0 {
		return "(none)"
	}
	out := searchers[0].Name()
	for _, s := range searchers[1:] {
		out += ", " + s.Name()
	}
	return out
}

func boolPtr(b bool) *bool { return &b }
