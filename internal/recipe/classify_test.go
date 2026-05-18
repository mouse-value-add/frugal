package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassify_StarterRecipesMatchExpectedTasks(t *testing.T) {
	r, err := Load(filepath.Join("..", "..", "config", "use_cases"))
	if err != nil {
		t.Skipf("starter dir not loadable: %v", err)
	}
	cases := []struct {
		task   string
		wantID string
	}{
		{"summarize recent advances in retrieval-augmented generation", "research-synthesis"},
		{"refactor this function to use generics", "code-dev"},
		{"who was the first president of france", "factual-qa"},
		{"extract speaker names from this transcript as JSON", "structured-extraction"},
		{"what are current iPhone prices today", "fresh-facts"},
	}
	for _, c := range cases {
		got, ok := r.Classify(c.task)
		if !ok {
			t.Errorf("Classify(%q): no match", c.task)
			continue
		}
		if got != c.wantID {
			t.Errorf("Classify(%q): got %q want %q", c.task, got, c.wantID)
		}
	}
}

func TestClassify_EmptyTaskReturnsFalse(t *testing.T) {
	r, _ := Load(filepath.Join("..", "..", "config", "use_cases"))
	if _, ok := r.Classify(""); ok {
		t.Errorf("Classify(\"\") should return false")
	}
}

func TestClassify_NoHintMatchReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	yaml := `id: only-recipe
description: only recipe
classifier:
  hints: [synthesize]
recipes:
  high:     { steps: [{ chat: { model: auto } }] }
  balanced: { steps: [{ chat: { model: auto } }] }
  cost:     { steps: [{ chat: { model: auto } }] }
`
	if err := os.WriteFile(filepath.Join(dir, "only.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := r.Classify("nothing relevant in here"); ok {
		t.Errorf("Classify with no matching hint should return false")
	}
}

func TestClassify_HigherScoreWinsOverEarlierLoadOrder(t *testing.T) {
	dir := t.TempDir()
	a := `id: a-recipe
description: a
classifier:
  hints: [alpha]
recipes:
  high:     { steps: [{ chat: { model: auto } }] }
  balanced: { steps: [{ chat: { model: auto } }] }
  cost:     { steps: [{ chat: { model: auto } }] }
`
	b := `id: b-recipe
description: b
classifier:
  hints: [alpha, beta, gamma]
recipes:
  high:     { steps: [{ chat: { model: auto } }] }
  balanced: { steps: [{ chat: { model: auto } }] }
  cost:     { steps: [{ chat: { model: auto } }] }
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(a), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// "alpha beta" matches a (1 hint) and b (2 hints) — b should win on score
	// even though a was loaded first.
	got, _ := r.Classify("alpha beta")
	if got != "b-recipe" {
		t.Errorf("Classify: got %q want b-recipe (higher score should beat load order)", got)
	}
	// "alpha" alone scores 1 for both — load order breaks the tie → a-recipe.
	got, _ = r.Classify("alpha")
	if got != "a-recipe" {
		t.Errorf("Classify: got %q want a-recipe (tie broken by load order)", got)
	}
}
