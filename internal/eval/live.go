package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/frugalsh/frugal/internal/classifier"
	"github.com/frugalsh/frugal/internal/config"
	"github.com/frugalsh/frugal/internal/provider"
	"github.com/frugalsh/frugal/internal/router"
	"github.com/frugalsh/frugal/internal/types"
)

// LiveRunner executes real ChatCompletion calls for each problem in a
// workload, scores the output with the problem's scorer, and tracks actual
// cost using per-token pricing pulled from the shipped config. It runs each
// problem twice — once through the Frugal router, once pinned to the
// workload's baseline model — so every problem has an apples-to-apples pair.
//
// Concurrency is deliberately left to the caller: the CLI layer runs
// sequentially today because provider rate limits dominate wall time. A
// goroutine pool can be layered on top without touching this struct.
type LiveRunner struct {
	Router     *router.Router
	Classifier classifier.Classifier
	Registry   *provider.Registry
	// ModelCosts maps model name → per-1k-token cost for input/output.
	// Populated from config.Config so the runner can compute real cost from
	// the usage numbers returned by providers.
	ModelCosts map[string]ModelCost
}

// ModelCost is the per-1k-token price a LiveRunner uses when billing a
// response. Mirrors the relevant subset of config.ModelConfig so the runner
// doesn't depend on the config package's full struct.
type ModelCost struct {
	InputPer1K  float64
	OutputPer1K float64
}

// NewLiveRunner builds a runner from a loaded config + registry, so the
// caller doesn't have to flatten the cost map by hand.
func NewLiveRunner(cfg *config.Config, cls classifier.Classifier, rtr *router.Router, reg *provider.Registry) *LiveRunner {
	costs := make(map[string]ModelCost)
	for _, pc := range cfg.Providers {
		for name, mc := range pc.Models {
			costs[name] = ModelCost{
				InputPer1K:  mc.CostPer1KInput,
				OutputPer1K: mc.CostPer1KOutput,
			}
		}
	}
	return &LiveRunner{Router: rtr, Classifier: cls, Registry: reg, ModelCosts: costs}
}

// LiveProblemResult holds one pair of (frugal, baseline) outcomes for a
// single problem. Costs are real (from provider-reported usage) when the
// provider returns a Usage block; fall back to router estimates otherwise.
type LiveProblemResult struct {
	ProblemID string
	// Frugal leg.
	FrugalModel     string
	FrugalProvider  string
	FrugalOutput    string
	FrugalPass      bool
	FrugalDetail    string
	FrugalCostUSD   float64
	FrugalLatencyMS int64
	FrugalErr       string
	// Baseline leg.
	BaselineModel     string
	BaselineOutput    string
	BaselinePass      bool
	BaselineDetail    string
	BaselineCostUSD   float64
	BaselineLatencyMS int64
	BaselineErr       string
}

// LiveSummary aggregates results across a workload run. Pass-rate is the
// share of problems each leg scored correct; cost is the total USD spent
// across all problems for that leg.
type LiveSummary struct {
	Workload         string
	Quality          types.QualityThreshold
	Baseline         string
	ProblemCount     int
	FrugalPassRate   float64
	BaselinePassRate float64
	FrugalCostUSD    float64
	BaselineCostUSD  float64
	SavingsPct       float64
	QualityDeltaPP   float64 // baseline pass-rate minus frugal pass-rate, in percentage points
	Results          []LiveProblemResult
	// ModelBreakdown: how often each model was selected by Frugal routing.
	ModelBreakdown map[string]int
}

// Run executes every problem in w sequentially. ctx is threaded into every
// upstream call so cancellation works. Problem-level errors (network,
// invalid response) are captured in the result rather than aborting the run:
// a noisy upstream shouldn't erase the whole benchmark report.
func (r *LiveRunner) Run(ctx context.Context, w LiveWorkload, quality types.QualityThreshold) (LiveSummary, error) {
	if _, err := r.Registry.Resolve(w.Baseline); err != nil {
		return LiveSummary{}, fmt.Errorf("baseline model %q not registered: %w", w.Baseline, err)
	}

	s := LiveSummary{
		Workload:       w.Name,
		Quality:        quality,
		Baseline:       w.Baseline,
		ProblemCount:   len(w.Problems),
		ModelBreakdown: map[string]int{},
	}

	var frugalPass, baselinePass int

	for _, p := range w.Problems {
		scorer, err := p.Scorer()
		if err != nil {
			// Already validated at load; guard anyway.
			return s, err
		}

		req := buildRequest(p)
		features := r.Classifier.Classify(req)
		decision := r.Router.Route(features, quality, nil)

		// --- Frugal leg ---
		pr := LiveProblemResult{ProblemID: p.ID}
		pr.FrugalModel = decision.SelectedModel
		pr.FrugalProvider = decision.SelectedProvider
		s.ModelBreakdown[decision.SelectedModel]++

		if decision.SelectedModel == "" {
			pr.FrugalErr = "router returned no model"
		} else if prov, err := r.Registry.Resolve(decision.SelectedModel); err != nil {
			pr.FrugalErr = err.Error()
		} else {
			pr.FrugalOutput, pr.FrugalCostUSD, pr.FrugalLatencyMS, pr.FrugalErr =
				r.callAndCost(ctx, prov, decision.SelectedModel, req)
			res := scorer.Score(pr.FrugalOutput)
			pr.FrugalPass = res.Pass
			pr.FrugalDetail = res.Detail
			if pr.FrugalPass {
				frugalPass++
			}
		}

		// --- Baseline leg ---
		pr.BaselineModel = w.Baseline
		if prov, err := r.Registry.Resolve(w.Baseline); err != nil {
			pr.BaselineErr = err.Error()
		} else {
			pr.BaselineOutput, pr.BaselineCostUSD, pr.BaselineLatencyMS, pr.BaselineErr =
				r.callAndCost(ctx, prov, w.Baseline, req)
			res := scorer.Score(pr.BaselineOutput)
			pr.BaselinePass = res.Pass
			pr.BaselineDetail = res.Detail
			if pr.BaselinePass {
				baselinePass++
			}
		}

		s.FrugalCostUSD += pr.FrugalCostUSD
		s.BaselineCostUSD += pr.BaselineCostUSD
		s.Results = append(s.Results, pr)
	}

	if s.ProblemCount > 0 {
		s.FrugalPassRate = float64(frugalPass) / float64(s.ProblemCount) * 100
		s.BaselinePassRate = float64(baselinePass) / float64(s.ProblemCount) * 100
	}
	s.QualityDeltaPP = s.BaselinePassRate - s.FrugalPassRate
	if s.BaselineCostUSD > 0 {
		s.SavingsPct = (s.BaselineCostUSD - s.FrugalCostUSD) / s.BaselineCostUSD * 100
	}

	return s, nil
}

// callAndCost runs one non-streaming ChatCompletion and extracts a string
// output + real cost from the Usage block. Real cost is preferred so reports
// reflect what the provider actually billed; if Usage is absent, fall back
// to zero rather than fabricating a number.
func (r *LiveRunner) callAndCost(ctx context.Context, prov provider.Provider, model string, req *types.ChatCompletionRequest) (output string, cost float64, latencyMS int64, errMsg string) {
	start := time.Now()
	resp, err := prov.ChatCompletion(ctx, model, req)
	latencyMS = time.Since(start).Milliseconds()
	if err != nil {
		return "", 0, latencyMS, err.Error()
	}
	if len(resp.Choices) == 0 {
		return "", 0, latencyMS, "empty choices"
	}
	output = extractText(resp.Choices[0].Message.Content)

	if resp.Usage != nil {
		mc := r.ModelCosts[model]
		cost = float64(resp.Usage.PromptTokens)/1000*mc.InputPer1K +
			float64(resp.Usage.CompletionTokens)/1000*mc.OutputPer1K
	}
	return output, cost, latencyMS, ""
}

func extractText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func buildRequest(p Problem) *types.ChatCompletionRequest {
	var msgs []types.Message
	if p.System != "" {
		content, _ := json.Marshal(p.System)
		msgs = append(msgs, types.Message{Role: "system", Content: content})
	}
	content, _ := json.Marshal(p.Prompt)
	msgs = append(msgs, types.Message{Role: "user", Content: content})

	req := &types.ChatCompletionRequest{
		Model:    "auto",
		Messages: msgs,
	}
	if p.JSONMode {
		req.ResponseFormat = &types.ResponseFormat{Type: "json_object"}
	}
	if p.MaxTokens > 0 {
		mt := p.MaxTokens
		req.MaxTokens = &mt
	}
	return req
}

// WriteLiveMarkdown renders the summary as a markdown report suitable for
// pasting into BENCHMARKS.md or a PR description.
func WriteLiveMarkdown(w interface{ Write(p []byte) (int, error) }, s LiveSummary) error {
	_, err := fmt.Fprintf(w, "# %s (quality=%s, baseline=%s)\n\n", s.Workload, s.Quality, s.Baseline)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Problems: %d  ·  Frugal pass: %.1f%%  ·  Baseline pass: %.1f%%  ·  Δ: %+.1fpp\n",
		s.ProblemCount, s.FrugalPassRate, s.BaselinePassRate, -s.QualityDeltaPP)
	fmt.Fprintf(w, "Cost: frugal $%.4f  ·  baseline $%.4f  ·  savings **%.1f%%**\n\n",
		s.FrugalCostUSD, s.BaselineCostUSD, s.SavingsPct)

	fmt.Fprintln(w, "## Model selection")
	var names []string
	for m := range s.ModelBreakdown {
		names = append(names, m)
	}
	sort.Slice(names, func(i, j int) bool { return s.ModelBreakdown[names[i]] > s.ModelBreakdown[names[j]] })
	for _, m := range names {
		fmt.Fprintf(w, "- `%s` × %d\n", m, s.ModelBreakdown[m])
	}

	fmt.Fprintln(w, "\n## Per-problem results")
	fmt.Fprintln(w, "| # | Problem | Frugal model | Frugal ✓ | Baseline ✓ |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	for i, r := range s.Results {
		fmt.Fprintf(w, "| %d | `%s` | `%s` | %s | %s |\n",
			i+1, r.ProblemID, r.FrugalModel, checkbox(r.FrugalPass), checkbox(r.BaselinePass))
	}
	return nil
}

func checkbox(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}
