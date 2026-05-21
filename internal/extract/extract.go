// Package extract defines the shared interface for routed
// content-extraction providers (go-readability, Firecrawl, …) and the
// data types the frugal__extract MCP tool exposes to clients.
//
// Provider drivers (internal/provider/goreadability,
// internal/provider/firecrawl) implement Extractor. The MCP tool handler
// in internal/mcp/tools picks one at call-time using the routing rule
// documented on CallWithFallback (which mirrors the search router).
//
// All numeric costs are USD per call.
package extract

import (
	"context"
	"time"
)

// Quoted is satisfied by providers that publish a recurring free-page
// budget (Firecrawl's ~500 pages/month, etc.). Same contract as
// search.Quoted; declared here so internal/extract has no dependency on
// internal/search.
type Quoted interface {
	EffectiveCostPerCall(now time.Time) float64
}

// EffectiveCostOf returns e.EffectiveCostPerCall(now) if e implements
// Quoted, otherwise e.CostPerCall(). Used by OrderByCost so routing
// decisions see the same effective price.
func EffectiveCostOf(e Extractor, now time.Time) float64 {
	if q, ok := e.(Quoted); ok {
		return q.EffectiveCostPerCall(now)
	}
	return e.CostPerCall()
}

// Extractor is the contract every extract-provider driver satisfies.
type Extractor interface {
	// Name reports the provider's identifier ("goreadability",
	// "firecrawl", …). Stable across releases; agents may pin via the
	// `provider` argument on frugal__extract.
	Name() string
	// CostPerCall is the published per-call USD price the operator
	// agreed to. Zero means free.
	CostPerCall() float64
	// Extract runs one extraction against q.URL. Implementations must
	// respect ctx for cancellation and request timeouts.
	Extract(ctx context.Context, q Query) (Result, error)
}

// Query is the input to one extract call. URL is required.
type Query struct {
	URL string
	// Formats indicates which output formats the caller wants.
	// Recognized: "markdown" (default), "html", "text". Drivers may
	// return additional formats opportunistically — empty means
	// "give me at least markdown."
	Formats []string
}

// Result carries the extracted content + per-call cost.
type Result struct {
	Markdown string
	HTML     string
	Text     string
	Title    string
	Byline   string
	Links    []string
	CostUSD  float64
}

// Find returns the extractor whose Name matches, or nil.
func Find(extractors []Extractor, name string) Extractor {
	for _, e := range extractors {
		if e.Name() == name {
			return e
		}
	}
	return nil
}
