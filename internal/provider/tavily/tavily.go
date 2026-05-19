// Package tavily implements a search.Searcher backed by the Tavily search
// API (https://tavily.com).
//
// Tavily is positioned as "LLM-tuned web search" — designed for use in
// retrieval-augmented chat flows, with a richer per-result content payload
// than raw SERP scrapers. Frugal picks Tavily by default for use cases
// that prioritize recall and snippet quality (research-synthesis,
// multi-source synthesis); Serper is preferred where per-call cost
// dominates and basic title+snippet is enough (factual-qa).
package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/frugalsh/frugal/internal/routing"
	"github.com/frugalsh/frugal/internal/search"
)

// DefaultBaseURL is the Tavily production endpoint.
const DefaultBaseURL = "https://api.tavily.com"

// Client implements search.Searcher against Tavily.
type Client struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	costPerCall float64
}

// New constructs a Tavily client. apiKey is the operator's Tavily key (the
// account paying for these calls). baseURL defaults to DefaultBaseURL when
// empty — overridable for tests against httptest. costPerCall is the
// per-search USD price the operator has agreed to with Tavily; the auto
// router uses it when deciding between providers.
func New(apiKey, baseURL string, costPerCall float64) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:      apiKey,
		baseURL:     baseURL,
		costPerCall: costPerCall,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
	}
}

// Name reports the provider identifier — stable across releases, used in
// MCP tool-call metadata and recipe YAML `provider:` overrides.
func (c *Client) Name() string { return "tavily" }

// CostPerCall returns the configured per-search USD price.
func (c *Client) CostPerCall() float64 { return c.costPerCall }

// Search runs one Tavily search. Returns search.Results with the per-call
// cost set to the configured price (Tavily doesn't return per-call cost in
// the response). Transient HTTP / network failures are retried inside the
// driver via routing.DoWithRetry; permanent failures (auth, bad query)
// surface immediately as *routing.Error with Kind=Permanent.
func (c *Client) Search(ctx context.Context, q search.Query) (search.Results, error) {
	if q.Text == "" {
		// Empty query is a caller bug — no point retrying or falling back.
		return search.Results{}, routing.Permanent(c.Name(), 0, fmt.Errorf("empty query"))
	}
	max := q.MaxResults
	if max <= 0 {
		max = 5
	}
	if max > 20 {
		max = 20
	}
	body := tavilyRequest{
		APIKey:      c.apiKey,
		Query:       q.Text,
		MaxResults:  max,
		SearchDepth: "basic",
	}
	if q.Freshness != "" {
		body.TimeRange = q.Freshness
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return search.Results{}, routing.Permanent(c.Name(), 0, fmt.Errorf("marshal request: %w", err))
	}

	var out search.Results
	err = routing.DoWithRetry(ctx, 1+len(routing.DefaultBackoff), routing.DefaultBackoff, func() error {
		var attemptErr error
		out, attemptErr = c.doOnce(ctx, buf)
		return attemptErr
	})
	return out, err
}

// doOnce runs one HTTP attempt against Tavily. The retry loop in Search
// wraps this; the returned error is already classified into
// *routing.Error so DoWithRetry can stop on permanent failures.
func (c *Client) doOnce(ctx context.Context, buf []byte) (search.Results, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/search", bytes.NewReader(buf))
	if err != nil {
		return search.Results{}, routing.Permanent(c.Name(), 0, fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return search.Results{}, &routing.Error{
			Provider: c.Name(), Kind: routing.ClassifyNetwork(err), Err: err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Tavily returns a JSON {"error": "..."} body on failure when content
		// type is JSON; otherwise the body is plain text. Cap the captured
		// body so a verbose 500 doesn't leak into telemetry.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return search.Results{}, &routing.Error{
			Provider: c.Name(),
			Kind:     routing.ClassifyHTTPStatus(resp.StatusCode),
			Status:   resp.StatusCode,
			Err:      fmt.Errorf("%s", bytes.TrimSpace(snippet)),
		}
	}

	var parsed tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		// 200 with malformed body is *probably* transient (provider hiccup,
		// truncated response) — give the router one more shot before giving up.
		return search.Results{}, routing.Transient(c.Name(), resp.StatusCode, fmt.Errorf("decode response: %w", err))
	}

	items := make([]search.Item, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		items = append(items, search.Item{
			Title:       r.Title,
			URL:         r.URL,
			Snippet:     r.Content,
			PublishedAt: r.PublishedDate,
		})
	}
	return search.Results{Items: items, CostUSD: c.costPerCall}, nil
}

type tavilyRequest struct {
	APIKey      string `json:"api_key"`
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth,omitempty"`
	TimeRange   string `json:"time_range,omitempty"`
}

type tavilyResponse struct {
	Results []struct {
		Title         string `json:"title"`
		URL           string `json:"url"`
		Content       string `json:"content"`
		PublishedDate string `json:"published_date,omitempty"`
	} `json:"results"`
}
