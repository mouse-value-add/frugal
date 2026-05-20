// Package browse defines the shared interface for routed
// headless-browser providers (Browserless, …) and the data types the
// frugal__browse MCP tool exposes to clients.
//
// "Browse" means: render a URL with a real JS-capable headless browser
// and return the rendered HTML (and optionally a plain-text view). It's
// the right tool when the page needs JS execution to populate content
// — when frugal__extract / go-readability returns empty content, the
// agent's next call should be frugal__browse.
//
// Provider drivers (internal/provider/browserless) implement Browser.
// The MCP tool handler in internal/mcp/tools picks one at call-time
// using the routing rule documented on CallWithFallback.
package browse

import (
	"context"
	"time"
)

// Quoted is satisfied by browsers that publish a recurring free-render
// budget (Browserless's free tier, etc.). Same contract as
// search.Quoted / extract.Quoted; declared here so internal/browse has
// no dependency on the other capability packages.
type Quoted interface {
	EffectiveCostPerCall(now time.Time) float64
}

// EffectiveCostOf returns b.EffectiveCostPerCall(now) if b implements
// Quoted, otherwise b.CostPerCall().
func EffectiveCostOf(b Browser, now time.Time) float64 {
	if q, ok := b.(Quoted); ok {
		return q.EffectiveCostPerCall(now)
	}
	return b.CostPerCall()
}

// Browser is the contract every browse-provider driver satisfies.
type Browser interface {
	// Name reports the provider's identifier ("browserless", …).
	// Stable across releases; agents may pin via the `provider` argument
	// on frugal__browse.
	Name() string
	// CostPerCall is the published per-render USD price the operator
	// agreed to. Zero means free.
	CostPerCall() float64
	// Render runs one headless render against q.URL. Implementations
	// must respect ctx for cancellation. Renders can take seconds —
	// drivers pick their own per-attempt timeout.
	Render(ctx context.Context, q Query) (Result, error)
}

// Query is the input to one render call. URL is required.
type Query struct {
	URL string
	// WaitForMS is an optional millisecond delay after the initial DOM
	// is ready, giving the page time to finish XHR / hydration. Zero
	// means "don't wait" (driver default).
	WaitForMS int
	// ReturnFormat picks the result shape. Recognized: "html" (default
	// — raw rendered HTML), "text" (HTML stripped to plain text). The
	// "screenshot_png" format is post-MVP.
	ReturnFormat string
}

// Result carries the rendered page + per-call cost.
type Result struct {
	HTML    string
	Text    string
	CostUSD float64
}

// Find returns the browser whose Name matches, or nil.
func Find(browsers []Browser, name string) Browser {
	for _, b := range browsers {
		if b.Name() == name {
			return b
		}
	}
	return nil
}
