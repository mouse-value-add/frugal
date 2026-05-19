package search

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/frugalsh/frugal/internal/routing"
)

// AttemptHook is the per-attempt observability hook used by
// CallWithFallback / CallPinned. Re-exported from internal/routing so
// the existing search.AttemptHook call sites keep working — the
// underlying type is identical.
type AttemptHook = routing.AttemptHook

// OrderByCost returns searchers sorted by effective cost ascending — the
// quota-aware price reported by Quoted.EffectiveCostPerCall when the
// driver implements it, falling back to the static CostPerCall otherwise.
// Stable — ties preserve input order, so callers can break ties by listing
// a preferred provider first. Returns a fresh slice; the input isn't
// mutated.
//
// All comparisons use a single `time.Now()` snapshot so a month-boundary
// rollover that lands mid-sort can't produce an inconsistent ordering.
func OrderByCost(searchers []Searcher) []Searcher {
	out := make([]Searcher, len(searchers))
	copy(out, searchers)
	now := time.Now()
	sort.SliceStable(out, func(i, j int) bool {
		return EffectiveCostOf(out[i], now) < EffectiveCostOf(out[j], now)
	})
	return out
}

// CallWithFallback walks searchers in cost order, calling Search on each
// until one succeeds. On transient error it logs and tries the next
// provider. On permanent error it returns immediately — falling back
// after a 401 just wastes the next provider's quota on the same broken
// query/auth/billing.
//
// Returns the searcher that produced the result, its Results, and nil
// error on success. On all-transient failure returns the last error
// (the one from the last attempted provider). On no searchers configured
// returns a non-nil error with no provider.
//
// logger is used for per-attempt logging at debug level (success) and
// warn level (fallback). Pass slog.Default() if no logger context is
// available. hook may be nil; when set, it's called after each attempt
// regardless of outcome.
func CallWithFallback(ctx context.Context, searchers []Searcher, q Query, logger *slog.Logger, hook AttemptHook) (Searcher, Results, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(searchers) == 0 {
		return nil, Results{}, errors.New("frugal: no search providers configured")
	}
	ordered := OrderByCost(searchers)
	var lastErr error
	for i, s := range ordered {
		start := time.Now()
		res, err := s.Search(ctx, q)
		latency := time.Since(start)
		if hook != nil {
			hook(s.Name(), latency, res.CostUSD, err)
		}
		if err == nil {
			logger.Debug("search ok",
				"provider", s.Name(),
				"cost_usd", res.CostUSD,
				"latency_ms", latency.Milliseconds(),
				"attempt", i+1,
				"results", len(res.Items))
			return s, res, nil
		}
		lastErr = err
		if routing.IsPermanent(err) {
			logger.Warn("search permanent error; aborting fallback chain",
				"provider", s.Name(),
				"latency_ms", latency.Milliseconds(),
				"err", err)
			return s, Results{}, err
		}
		logger.Warn("search transient error; falling back",
			"provider", s.Name(),
			"attempt", i+1,
			"remaining", len(ordered)-i-1,
			"latency_ms", latency.Milliseconds(),
			"err", err)
	}
	return ordered[len(ordered)-1], Results{}, lastErr
}

// CallPinned dispatches one call against the named searcher only — no
// fallback. Used when the recipe / tool-call argument pins a specific
// provider. Returns ErrProviderNotConfigured if name doesn't match any
// known searcher. hook may be nil.
func CallPinned(ctx context.Context, searchers []Searcher, name string, q Query, logger *slog.Logger, hook AttemptHook) (Searcher, Results, error) {
	s := Find(searchers, name)
	if s == nil {
		return nil, Results{}, &ErrProviderNotConfigured{Name: name, Known: namesOf(searchers)}
	}
	if logger == nil {
		logger = slog.Default()
	}
	start := time.Now()
	res, err := s.Search(ctx, q)
	latency := time.Since(start)
	if hook != nil {
		hook(s.Name(), latency, res.CostUSD, err)
	}
	if err != nil {
		logger.Warn("search pinned error",
			"provider", s.Name(),
			"latency_ms", latency.Milliseconds(),
			"permanent", routing.IsPermanent(err),
			"err", err)
	}
	return s, res, err
}

// ErrProviderNotConfigured is returned by CallPinned when the requested
// provider isn't in the configured set. Holds the list of valid names
// so callers can produce a helpful error message.
type ErrProviderNotConfigured struct {
	Name  string
	Known []string
}

func (e *ErrProviderNotConfigured) Error() string {
	if len(e.Known) == 0 {
		return "provider " + e.Name + " not configured (no providers configured)"
	}
	return "provider " + e.Name + " not configured (known: " + joinNames(e.Known) + ")"
}

func namesOf(searchers []Searcher) []string {
	out := make([]string, 0, len(searchers))
	for _, s := range searchers {
		out = append(out, s.Name())
	}
	return out
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}
