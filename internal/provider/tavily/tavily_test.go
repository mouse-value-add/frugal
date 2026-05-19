package tavily

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frugalsh/frugal/internal/routing"
	"github.com/frugalsh/frugal/internal/search"
)

func TestSearch_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing content-type header")
		}
		body, _ := io.ReadAll(r.Body)
		var got tavilyRequest
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got.APIKey != "tvly-test" {
			t.Errorf("api_key: got %q want tvly-test", got.APIKey)
		}
		if got.Query != "current iPhone prices" {
			t.Errorf("query: got %q", got.Query)
		}
		if got.MaxResults != 3 {
			t.Errorf("max_results: got %d want 3", got.MaxResults)
		}
		_, _ = w.Write([]byte(`{
		  "results":[
		    {"title":"Apple iPhone 17","url":"https://apple.com/iphone","content":"Starting at $799","published_date":"2026-05-10"},
		    {"title":"Best Buy","url":"https://bestbuy.com","content":"iPhone 17 from $749"}
		  ]
		}`))
	}))
	defer srv.Close()

	c := New("tvly-test", srv.URL, 0.008)
	res, err := c.Search(context.Background(), search.Query{Text: "current iPhone prices", MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(res.Items))
	}
	if res.Items[0].Title != "Apple iPhone 17" || res.Items[0].URL != "https://apple.com/iphone" {
		t.Errorf("item[0] = %+v", res.Items[0])
	}
	if res.Items[0].PublishedAt != "2026-05-10" {
		t.Errorf("published_at: got %q", res.Items[0].PublishedAt)
	}
	if res.CostUSD != 0.008 {
		t.Errorf("cost: got %v want 0.008", res.CostUSD)
	}
}

func TestSearch_EmptyQueryRejected(t *testing.T) {
	c := New("tvly-test", "http://does-not-matter", 0.008)
	if _, err := c.Search(context.Background(), search.Query{}); err == nil {
		t.Fatalf("expected error for empty query")
	}
}

func TestSearch_HTTPErrorSurfacedWithSnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()
	c := New("bad", srv.URL, 0.008)
	_, err := c.Search(context.Background(), search.Query{Text: "x"})
	if err == nil {
		t.Fatalf("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error message should include status and snippet, got: %v", err)
	}
	// 401 is auth — must be permanent so the router doesn't waste another
	// provider's quota retrying the same bad credential.
	if !routing.IsPermanent(err) {
		t.Errorf("401 must classify as permanent; got %v", err)
	}
}

func TestSearch_429ClassifiedTransient(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()
	c := New("k", srv.URL, 0.008)
	// Override the backoff so the test doesn't actually sleep ~1s between
	// retries — we just want to confirm retry behavior.
	c.httpClient.Timeout = 2 * time.Second
	_, err := c.Search(context.Background(), search.Query{Text: "x"})
	if err == nil {
		t.Fatalf("expected transient error after retries exhausted")
	}
	if !routing.IsTransient(err) {
		t.Errorf("429 must classify as transient; got %v", err)
	}
	// Default schedule retries 2x (3 attempts total). The retry budget is
	// intentionally tight so we only assert "more than one call happened"
	// — the exact number is a knob we may tune.
	if calls.Load() < 2 {
		t.Errorf("expected >=2 attempts on transient 429; got %d", calls.Load())
	}
}

func TestSearch_NetworkErrorClassifiedTransient(t *testing.T) {
	// Point at a closed listener: connect refused → net.OpError → transient.
	c := New("k", "http://127.0.0.1:1", 0.008)
	c.httpClient.Timeout = 200 * time.Millisecond
	_, err := c.Search(context.Background(), search.Query{Text: "x"})
	if err == nil {
		t.Fatalf("expected network error")
	}
	if !routing.IsTransient(err) {
		t.Errorf("network failure must classify as transient; got %v", err)
	}
}

func TestSearch_RetrySucceedsAfterTransient(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// First attempt: 503. Retry should succeed.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`upstream blip`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"OK","url":"https://x","content":"hit"}]}`))
	}))
	defer srv.Close()
	c := New("k", srv.URL, 0.008)
	res, err := c.Search(context.Background(), search.Query{Text: "x"})
	if err != nil {
		t.Fatalf("expected retry to recover; got %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected exactly 2 attempts (1 fail + 1 success); got %d", calls.Load())
	}
	if len(res.Items) != 1 || res.Items[0].Title != "OK" {
		t.Errorf("unexpected results after retry: %+v", res.Items)
	}
}

func TestSearch_EmptyQueryIsPermanent(t *testing.T) {
	c := New("k", "http://example.invalid", 0.008)
	_, err := c.Search(context.Background(), search.Query{})
	if err == nil {
		t.Fatalf("expected error for empty query")
	}
	if !routing.IsPermanent(err) {
		t.Errorf("empty-query error should be permanent (no point falling back); got %v", err)
	}
	// Sanity: errors.As reaches the typed error.
	var typed *routing.Error
	if !errors.As(err, &typed) || typed.Provider != "tavily" {
		t.Errorf("expected *routing.Error from tavily; got %v", err)
	}
}

func TestNameAndCost(t *testing.T) {
	c := New("k", "", 0.008)
	if c.Name() != "tavily" {
		t.Errorf("Name: got %q", c.Name())
	}
	if c.CostPerCall() != 0.008 {
		t.Errorf("CostPerCall: got %v", c.CostPerCall())
	}
}

func TestNew_DefaultsBaseURL(t *testing.T) {
	c := New("k", "", 0.008)
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL default: got %q want %q", c.baseURL, DefaultBaseURL)
	}
}
