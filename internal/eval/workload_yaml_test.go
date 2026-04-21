package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLiveWorkload_StarterReturnsAllProblems(t *testing.T) {
	// Resolve relative to the repo root so tests are runnable from any cwd
	// that Go's test harness lands on.
	path := filepath.Join("..", "..", "config", "workloads", "starter.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("starter workload not found: %v", err)
	}

	w, err := LoadLiveWorkload(path)
	if err != nil {
		t.Fatalf("LoadLiveWorkload: %v", err)
	}
	if w.Name != "starter" {
		t.Errorf("expected name=starter, got %q", w.Name)
	}
	if w.Baseline == "" {
		t.Errorf("expected non-empty baseline model")
	}
	if len(w.Problems) < 15 {
		t.Errorf("expected >= 15 problems in starter workload, got %d", len(w.Problems))
	}

	// Every problem must build a valid scorer.
	for _, p := range w.Problems {
		if _, err := p.Scorer(); err != nil {
			t.Errorf("problem %q scorer build: %v", p.ID, err)
		}
	}
}

func TestLoadLiveWorkload_RejectsMultipleExpectedFields(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte(`
name: bad
baseline: mock-model
problems:
  - id: p1
    prompt: hello
    expected_equals: "42"
    expected_contains: "forty-two"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadLiveWorkload(bad); err == nil {
		t.Fatalf("expected error when multiple expected_* fields are set")
	}
}

func TestLoadLiveWorkload_RejectsDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "dup.yaml")
	if err := os.WriteFile(bad, []byte(`
name: dup
baseline: mock-model
problems:
  - id: same
    prompt: hello
    expected_equals: hi
  - id: same
    prompt: world
    expected_equals: world
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadLiveWorkload(bad); err == nil {
		t.Fatalf("expected error on duplicate problem ids")
	}
}
