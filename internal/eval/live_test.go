package eval

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/frugalsh/frugal/internal/classifier"
	"github.com/frugalsh/frugal/internal/provider"
	"github.com/frugalsh/frugal/internal/router"
	"github.com/frugalsh/frugal/internal/types"
)

// mockProv returns a canned response per model so we can exercise both the
// Frugal and baseline legs deterministically without a network.
type mockProv struct {
	responses map[string]string
}

func (m *mockProv) Name() string     { return "mock" }
func (m *mockProv) Models() []string { return []string{"cheap", "expensive"} }
func (m *mockProv) ChatCompletion(ctx context.Context, model string, req *types.ChatCompletionRequest) (*types.ChatCompletionResponse, error) {
	text, ok := m.responses[model]
	if !ok {
		text = ""
	}
	content, _ := json.Marshal(text)
	fr := "stop"
	return &types.ChatCompletionResponse{
		ID:      "test-" + model,
		Object:  "chat.completion",
		Model:   model,
		Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: content}, FinishReason: &fr}},
		Usage:   &types.Usage{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25},
	}, nil
}
func (m *mockProv) ChatCompletionStream(ctx context.Context, model string, req *types.ChatCompletionRequest) (<-chan provider.StreamChunk, error) {
	return nil, nil
}

func TestLiveRunner_ScoresBothLegsAndComputesDelta(t *testing.T) {
	reg := provider.NewRegistry()
	// cheap answers wrong on math, right on classify; expensive is always right.
	reg.Register(&mockProv{responses: map[string]string{
		"cheap":     "positive",         // correct for classify; wrong for math (no number)
		"expensive": "The answer is 42", // contains 42 for math; wrong word for classify
	}})

	models := []router.ModelEntry{
		{
			Name: "cheap", Provider: "mock",
			CostPer1KInput: 0.0001, CostPer1KOutput: 0.0004,
			Reasoning: 0.5, Coding: 0.5, Creative: 0.5, InstructFollowing: 0.5,
			MaxContext: 10000,
		},
		{
			Name: "expensive", Provider: "mock",
			CostPer1KInput: 0.003, CostPer1KOutput: 0.015,
			Reasoning: 0.95, Coding: 0.95, Creative: 0.95, InstructFollowing: 0.95,
			MaxContext: 10000,
		},
	}
	thresholds := map[string]router.Threshold{
		"cost":     {},
		"balanced": {MinReasoning: 0.3, MinCoding: 0.3, MinCreative: 0.3, MinInstructFollowing: 0.3},
	}

	runner := &LiveRunner{
		Router:     router.New(models, thresholds),
		Classifier: classifier.NewRuleBased(),
		Registry:   reg,
		ModelCosts: map[string]ModelCost{
			"cheap":     {InputPer1K: 0.0001, OutputPer1K: 0.0004},
			"expensive": {InputPer1K: 0.003, OutputPer1K: 0.015},
		},
	}

	workload := LiveWorkload{
		Name:     "unit",
		Baseline: "expensive",
		Problems: []Problem{
			{ID: "classify-pos", Prompt: "is it nice?", ExpectedEquals: "positive"},
			{ID: "math-forty-two", Prompt: "What is 6*7?", ExpectedContains: "42"},
		},
	}

	s, err := runner.Run(context.Background(), workload, types.QualityCost)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if s.ProblemCount != 2 {
		t.Fatalf("want 2 problems, got %d", s.ProblemCount)
	}
	// At quality=cost, Frugal should prefer "cheap".
	if s.ModelBreakdown["cheap"] != 2 {
		t.Errorf("expected Frugal to pick cheap twice, breakdown=%v", s.ModelBreakdown)
	}
	// Baseline (expensive) contains "42" — correct on math; wrong on classify.
	// Cheap returns "positive" — right on classify, wrong on math.
	// Each leg scores 1/2 = 50%.
	if s.FrugalPassRate != 50.0 {
		t.Errorf("expected FrugalPassRate=50, got %.1f", s.FrugalPassRate)
	}
	if s.BaselinePassRate != 50.0 {
		t.Errorf("expected BaselinePassRate=50, got %.1f", s.BaselinePassRate)
	}
	// Costs computed from Usage × per-token rates.
	if s.BaselineCostUSD <= s.FrugalCostUSD {
		t.Errorf("expected baseline cost to exceed frugal cost; frugal=%.6f baseline=%.6f",
			s.FrugalCostUSD, s.BaselineCostUSD)
	}
	if s.SavingsPct <= 0 {
		t.Errorf("expected positive savings, got %.2f%%", s.SavingsPct)
	}
}

func TestLiveRunner_RejectsUnregisteredBaseline(t *testing.T) {
	runner := &LiveRunner{
		Registry:   provider.NewRegistry(),
		Classifier: classifier.NewRuleBased(),
		Router:     router.New(nil, nil),
	}
	_, err := runner.Run(context.Background(), LiveWorkload{Name: "x", Baseline: "nope", Problems: []Problem{{ID: "p1", Prompt: "hi", ExpectedContains: "hi"}}}, types.QualityCost)
	if err == nil {
		t.Fatalf("expected error when baseline is unregistered")
	}
}
