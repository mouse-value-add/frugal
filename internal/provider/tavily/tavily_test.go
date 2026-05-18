package tavily

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
