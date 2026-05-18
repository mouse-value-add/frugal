package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_StarterRecipesRegister(t *testing.T) {
	dir := filepath.Join("..", "..", "config", "use_cases")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("starter use_cases dir not present: %v", err)
	}
	r, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"code-dev", "factual-qa", "fresh-facts", "research-synthesis", "structured-extraction"}
	got := r.IDs()
	if len(got) != len(want) {
		t.Fatalf("expected %d recipes, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ids[%d]: got %q want %q", i, got[i], want[i])
		}
	}
	// Every starter recipe must declare all three tiers with at least one
	// step, and the first chat step (if any) must name a model.
	for _, id := range want {
		rec, ok := r.Get(id)
		if !ok {
			t.Fatalf("expected %q to be registered", id)
		}
		for _, tier := range ValidTiers {
			tr, ok := rec.Recipes[tier]
			if !ok {
				t.Errorf("%s: missing tier %q", id, tier)
				continue
			}
			if len(tr.Steps) == 0 {
				t.Errorf("%s@%s: no steps", id, tier)
			}
		}
	}
}

func TestTier_UnknownRecipeReturnsFalse(t *testing.T) {
	r, err := Load(filepath.Join("..", "..", "config", "use_cases"))
	if err != nil {
		t.Skipf("starter dir not loadable: %v", err)
	}
	if _, ok := r.Tier("does-not-exist", "balanced"); ok {
		t.Fatalf("expected false for unknown recipe")
	}
}

func TestLoad_RejectsStepWithBothToolAndChat(t *testing.T) {
	dir := t.TempDir()
	yaml := `id: broken
description: invalid step
classifier:
  hints: [a]
recipes:
  high:
    steps:
      - tool: frugal__search
        chat: { model: auto }
  balanced:
    steps:
      - chat: { model: auto }
  cost:
    steps:
      - chat: { model: auto }
`
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatalf("expected load error for tool+chat step")
	}
}

func TestLoad_RejectsMissingTier(t *testing.T) {
	dir := t.TempDir()
	yaml := `id: incomplete
description: missing cost tier
classifier:
  hints: [a]
recipes:
  high:
    steps: [{ chat: { model: auto } }]
  balanced:
    steps: [{ chat: { model: auto } }]
`
	if err := os.WriteFile(filepath.Join(dir, "incomplete.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatalf("expected load error for missing cost tier")
	}
}

func TestLoad_RejectsUnknownTopLevelField(t *testing.T) {
	dir := t.TempDir()
	yaml := `id: typo
description: typo
classifier:
  hints: [a]
recipes:
  high:    { steps: [{ chat: { model: auto } }] }
  balanced: { steps: [{ chat: { model: auto } }] }
  cost:     { steps: [{ chat: { model: auto } }] }
mystery_field: oops
`
	if err := os.WriteFile(filepath.Join(dir, "typo.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatalf("expected load error for unknown top-level field")
	}
}

func TestTierRecipe_AccessorsExtractFromSteps(t *testing.T) {
	tr := TierRecipe{
		Steps: []Step{
			{Tool: "frugal__search", With: map[string]any{"query": "{task}"}},
			{Chat: &ChatStep{Model: "gpt-4.1-mini", System: "answer"}},
		},
	}
	if got := tr.ChatModel(); got != "gpt-4.1-mini" {
		t.Errorf("ChatModel: got %q, want %q", got, "gpt-4.1-mini")
	}
	if !tr.HasToolStep("frugal__search") {
		t.Errorf("HasToolStep(search) = false, want true")
	}
	if tr.HasToolStep("frugal__browse") {
		t.Errorf("HasToolStep(browse) = true, want false (no such step)")
	}
	if (TierRecipe{}).ChatModel() != "" {
		t.Errorf("ChatModel on empty tier should be empty")
	}
}
