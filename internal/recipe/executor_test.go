package recipe

import (
	"context"
	"strings"
	"testing"
)

type fakeTools struct {
	called    int
	args      map[string]any
	structure map[string]any
}

func (f *fakeTools) CallTool(_ context.Context, _ string, args map[string]any) (ToolResult, error) {
	f.called++
	f.args = args
	return ToolResult{Structured: f.structure}, nil
}

type fakeChat struct {
	resolved   string
	called     int
	gotSystem  string
	gotInput   string
	gotModel   string
	gotQuality string
	output     string
	cost       float64
}

func (f *fakeChat) Resolve(req ChatRequest) string {
	if f.resolved != "" {
		return f.resolved
	}
	return req.Model
}
func (f *fakeChat) Chat(_ context.Context, req ChatRequest) (ChatResponse, error) {
	f.called++
	f.gotSystem = req.System
	f.gotInput = req.Input
	f.gotModel = req.Model
	f.gotQuality = req.Quality
	model := req.Model
	if model == "auto" && f.resolved != "" {
		model = f.resolved
	}
	return ChatResponse{Model: model, Output: f.output, CostUSD: f.cost, LatencyMS: 12}, nil
}

func freshFactsRecipe() Recipe {
	return Recipe{
		ID: "fresh-facts",
		Recipes: map[string]TierRecipe{
			"balanced": {
				Steps: []Step{
					{
						Tool: "frugal__search",
						With: map[string]any{"query": "{task}", "max_results": 3},
					},
					{
						Chat: &ChatStep{
							Model:  "auto",
							System: "Answer using only the supplied search results.",
							Input:  "{task}\n\nResults:\n{step1.results}",
						},
					},
				},
				Reason: "search + small model",
			},
		},
	}
}

func TestExecute_ThreadsToolOutputIntoChat(t *testing.T) {
	tools := &fakeTools{structure: map[string]any{
		"results": []any{
			map[string]any{"title": "Apple iPhone 17", "url": "https://apple.com", "snippet": "Starts at $799"},
			map[string]any{"title": "Best Buy", "url": "https://bestbuy.com", "snippet": "iPhone 17 from $749"},
		},
		"cost_usd":      0.0003,
		"provider_used": "serper",
		"latency_ms":    float64(120),
	}}
	chat := &fakeChat{resolved: "gpt-4.1-nano", output: "Around $749-$799.", cost: 0.0005}

	exec := NewExecutor(tools, chat)
	res, err := exec.Execute(context.Background(), freshFactsRecipe(), "balanced", "current iPhone prices")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tools.called != 1 {
		t.Errorf("tools called %d times, want 1", tools.called)
	}
	if got := tools.args["query"]; got != "current iPhone prices" {
		t.Errorf("tool query: got %q want %q", got, "current iPhone prices")
	}
	if chat.called != 1 {
		t.Errorf("chat called %d times, want 1", chat.called)
	}
	// The {step1.results} placeholder must be replaced with the formatted
	// numbered list, not the literal "{step1.results}".
	if strings.Contains(chat.gotInput, "{step1.results}") {
		t.Errorf("chat input still contains placeholder: %q", chat.gotInput)
	}
	if !strings.Contains(chat.gotInput, "Apple iPhone 17") {
		t.Errorf("chat input missing first search title; got: %q", chat.gotInput)
	}
	if !strings.Contains(chat.gotInput, "current iPhone prices") {
		t.Errorf("chat input missing task; got: %q", chat.gotInput)
	}
	if res.FinalOutput != "Around $749-$799." {
		t.Errorf("FinalOutput: got %q", res.FinalOutput)
	}
	wantCost := 0.0003 + 0.0005
	if res.TotalCostUSD < wantCost-1e-9 || res.TotalCostUSD > wantCost+1e-9 {
		t.Errorf("TotalCostUSD: got %v want %v", res.TotalCostUSD, wantCost)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("steps: got %d want 2", len(res.Steps))
	}
	if res.Steps[0].Provider != "serper" {
		t.Errorf("step1.Provider: got %q want serper", res.Steps[0].Provider)
	}
	if res.Steps[1].Name != "gpt-4.1-nano" {
		t.Errorf("step2.Name: got %q want gpt-4.1-nano (resolved from auto)", res.Steps[1].Name)
	}
}

func TestExecute_ChatErrorStopsRecipe(t *testing.T) {
	tools := &fakeTools{structure: map[string]any{"results": []any{}, "cost_usd": 0.0}}
	chat := &errorChat{}
	exec := NewExecutor(tools, chat)
	_, err := exec.Execute(context.Background(), freshFactsRecipe(), "balanced", "x")
	if err == nil {
		t.Fatalf("expected chat error to surface")
	}
	if !strings.Contains(err.Error(), "step 2") {
		t.Errorf("error should mention step 2; got %v", err)
	}
}

type errorChat struct{}

func (errorChat) Resolve(_ ChatRequest) string { return "auto" }
func (errorChat) Chat(_ context.Context, _ ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, &chatErr{}
}

type chatErr struct{}

func (chatErr) Error() string { return "upstream rejected" }

func TestPlan_PreviewsResolvedSteps(t *testing.T) {
	tools := &fakeTools{}
	chat := &fakeChat{resolved: "gpt-4.1-nano"}
	exec := NewExecutor(tools, chat)
	plan, err := exec.Plan(freshFactsRecipe(), "balanced", "current iPhone prices")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if tools.called != 0 || chat.called != 0 {
		t.Errorf("Plan must not call tools/chat; got tools=%d chat=%d", tools.called, chat.called)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("plan steps: got %d want 2", len(plan.Steps))
	}
	if plan.Steps[0].Kind != "tool" || plan.Steps[0].Name != "frugal__search" {
		t.Errorf("step1: %+v", plan.Steps[0])
	}
	if got := plan.Steps[0].Args["query"]; got != "current iPhone prices" {
		t.Errorf("step1.Args[query]: got %q (placeholder should be resolved)", got)
	}
	if plan.Steps[1].Kind != "chat" || plan.Steps[1].Name != "gpt-4.1-nano" {
		t.Errorf("step2: %+v (Name should be resolved from auto)", plan.Steps[1])
	}
	// {step1.results} is still a placeholder in the plan preview — step 1
	// hasn't run, so there's nothing to substitute.
	if !strings.Contains(plan.Steps[1].Input, "{step1.results}") {
		t.Errorf("plan preview should leave {step1.results} placeholder intact; got %q", plan.Steps[1].Input)
	}
}

func TestExecute_UnknownTierError(t *testing.T) {
	exec := NewExecutor(&fakeTools{}, &fakeChat{})
	_, err := exec.Execute(context.Background(), freshFactsRecipe(), "premium", "x")
	if err == nil || !strings.Contains(err.Error(), "premium") {
		t.Errorf("expected error mentioning premium tier; got %v", err)
	}
}

func TestResolveString_UnknownKeyStaysLiteral(t *testing.T) {
	tc := &templateContext{task: "hi"}
	got := resolveString("hello {task}, see {unknown.key}", tc)
	want := "hello hi, see {unknown.key}"
	if got != want {
		t.Errorf("resolveString: got %q want %q", got, want)
	}
}

func TestFormatValue_ResultListRendersNumbered(t *testing.T) {
	v := []any{
		map[string]any{"title": "A", "url": "https://a", "snippet": "snippet-a"},
		map[string]any{"title": "B"},
	}
	got := formatValue(v)
	if !strings.Contains(got, "[1] A") || !strings.Contains(got, "https://a") || !strings.Contains(got, "[2] B") {
		t.Errorf("formatValue did not render numbered list: %q", got)
	}
}
