package obs

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecordCall_AggregatesPerProvider(t *testing.T) {
	m := NewMetrics()
	m.EnsureProvider("tavily")
	m.RecordCall("tavily", 120*time.Millisecond, 0.008, nil)
	m.RecordCall("tavily", 200*time.Millisecond, 0.008, nil)
	m.RecordCall("tavily", 300*time.Millisecond, 0, errors.New("blip"))
	m.RecordCall("searxng", 30*time.Millisecond, 0, nil)

	snap := m.Snapshot()
	if len(snap.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(snap.Providers))
	}
	// Sorted by name.
	if snap.Providers[0].Name != "searxng" || snap.Providers[1].Name != "tavily" {
		t.Errorf("provider order: %v", []string{snap.Providers[0].Name, snap.Providers[1].Name})
	}
	tav := snap.Providers[1]
	if tav.Calls != 3 {
		t.Errorf("tavily.Calls = %d, want 3", tav.Calls)
	}
	if tav.Errors != 1 {
		t.Errorf("tavily.Errors = %d, want 1", tav.Errors)
	}
	if tav.CostUSD != 0.016 {
		// Two successful 0.008 calls = 0.016; the error call's cost=0.
		t.Errorf("tavily.CostUSD = %v, want 0.016", tav.CostUSD)
	}
	if tav.AvgLatencyMS != (120+200+300)/3 {
		t.Errorf("tavily.AvgLatencyMS = %d, want %d", tav.AvgLatencyMS, (120+200+300)/3)
	}
	if snap.TotalCost != 0.016 {
		t.Errorf("TotalCost = %v, want 0.016", snap.TotalCost)
	}
}

func TestHasActivity(t *testing.T) {
	m := NewMetrics()
	m.EnsureProvider("tavily")
	if m.HasActivity() {
		t.Errorf("HasActivity should be false before any call")
	}
	m.RecordCall("tavily", time.Millisecond, 0.001, nil)
	if !m.HasActivity() {
		t.Errorf("HasActivity should be true after a call")
	}
}

func TestMonthlyCalls_IncrementsAndIsolatesProviders(t *testing.T) {
	m := NewMetrics()
	m.RecordCall("tavily", time.Millisecond, 0.008, nil)
	m.RecordCall("tavily", time.Millisecond, 0.008, nil)
	m.RecordCall("serper", time.Millisecond, 0.001, nil)
	if got := m.MonthlyCalls("tavily"); got != 2 {
		t.Errorf("tavily MonthlyCalls = %d, want 2", got)
	}
	if got := m.MonthlyCalls("serper"); got != 1 {
		t.Errorf("serper MonthlyCalls = %d, want 1", got)
	}
	if got := m.MonthlyCalls("unknown"); got != 0 {
		t.Errorf("unknown MonthlyCalls = %d, want 0", got)
	}
}

func TestMonthlyCalls_ResetsOnMonthRollover(t *testing.T) {
	// We can't easily warp wall-clock time, but ProviderStats.bumpMonthly
	// accepts a clock argument so the rollover code is exercise-able
	// directly. Verify the reset behavior by driving the internal helper.
	ps := &ProviderStats{}
	prev := time.Date(2026, time.March, 31, 23, 59, 0, 0, time.UTC)
	ps.bumpMonthly(prev)
	ps.bumpMonthly(prev)
	if ps.MonthlyCalls != 2 {
		t.Fatalf("MonthlyCalls before rollover = %d, want 2", ps.MonthlyCalls)
	}
	next := time.Date(2026, time.April, 1, 0, 0, 1, 0, time.UTC)
	ps.bumpMonthly(next)
	if ps.MonthlyCalls != 1 {
		t.Errorf("MonthlyCalls after rollover = %d, want 1 (reset + this call)", ps.MonthlyCalls)
	}
	// Lifetime Calls counter is unaffected by rollover. (We didn't add to
	// it here — that's the RecordCall path. Just confirm the rollover
	// didn't leak into it.)
	if got := ps.Calls.Load(); got != 0 {
		t.Errorf("lifetime Calls touched by rollover; got %d", got)
	}
}

func TestMonthlyCalls_ConcurrentSafe(t *testing.T) {
	m := NewMetrics()
	const goroutines = 16
	const perGoroutine = 100
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				m.RecordCall("tavily", time.Microsecond, 0.001, nil)
			}
		}()
	}
	wg.Wait()
	if got := m.MonthlyCalls("tavily"); got != goroutines*perGoroutine {
		t.Errorf("MonthlyCalls = %d, want %d (no lost updates under contention)", got, goroutines*perGoroutine)
	}
}

func TestWritePrometheus(t *testing.T) {
	m := NewMetrics()
	m.RecordCall("tavily", 100*time.Millisecond, 0.008, nil)
	m.RecordCall("tavily", 200*time.Millisecond, 0, errors.New("x"))
	m.RecordCall("searxng", 20*time.Millisecond, 0, nil)

	var buf bytes.Buffer
	if err := m.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	out := buf.String()

	mustContain := []string{
		`# TYPE frugal_search_calls_total counter`,
		`frugal_search_calls_total{provider="tavily"} 2`,
		`frugal_search_calls_total{provider="searxng"} 1`,
		`# TYPE frugal_search_errors_total counter`,
		`frugal_search_errors_total{provider="tavily"} 1`,
		`# TYPE frugal_search_cost_usd_total counter`,
		`frugal_search_cost_usd_total{provider="tavily"} 0.008000`,
		`frugal_search_cost_usd_total{provider="searxng"} 0.000000`,
		`# TYPE frugal_search_latency_ms_avg gauge`,
		`frugal_search_latency_ms_avg{provider="tavily"} 150`,
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("Prometheus output missing %q. Full output:\n%s", want, out)
		}
	}
}
