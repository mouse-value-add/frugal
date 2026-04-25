package eval

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Problem is one benchmark item: a prompt, optional system prompt, and one
// scorer selected from a small in-tree palette (see scorer.go). Keeping the
// scorer palette small on purpose — LLM-judge scorers can be added later
// behind a type: "judge" branch without breaking existing workloads.
type Problem struct {
	ID          string   `yaml:"id"`
	Prompt      string   `yaml:"prompt"`
	System      string   `yaml:"system,omitempty"`
	JSONMode    bool     `yaml:"json_mode,omitempty"`
	MaxTokens   int      `yaml:"max_tokens,omitempty"`

	// Exactly one of the Expected* fields should be set per problem. The YAML
	// loader infers the scorer type from whichever one is non-zero.
	ExpectedEquals      string   `yaml:"expected_equals,omitempty"`
	ExpectedContains    string   `yaml:"expected_contains,omitempty"`
	ExpectedContainsAll []string `yaml:"expected_contains_all,omitempty"`
	ExpectedKeys        []string `yaml:"expected_keys,omitempty"`
	ExpectedNumber      *float64 `yaml:"expected_number,omitempty"`

	CaseFold     bool    `yaml:"case_fold,omitempty"`
	Tolerance    float64 `yaml:"tolerance,omitempty"`
}

// Scorer builds the appropriate Scorer for this problem. Returns an error if
// zero or more than one Expected* field is set — workloads should fail loudly
// on malformed rows rather than silently scoring every response as passing.
func (p Problem) Scorer() (Scorer, error) {
	picks := 0
	if p.ExpectedEquals != "" {
		picks++
	}
	if p.ExpectedContains != "" {
		picks++
	}
	if len(p.ExpectedContainsAll) > 0 {
		picks++
	}
	if len(p.ExpectedKeys) > 0 {
		picks++
	}
	if p.ExpectedNumber != nil {
		picks++
	}
	if picks == 0 {
		return nil, fmt.Errorf("problem %q has no expected_* scorer field", p.ID)
	}
	if picks > 1 {
		return nil, fmt.Errorf("problem %q has multiple expected_* fields; pick one", p.ID)
	}

	switch {
	case p.ExpectedEquals != "":
		return ExactTrimmed{Expected: p.ExpectedEquals}, nil
	case p.ExpectedContains != "":
		return Substring{Expected: p.ExpectedContains, CaseFold: p.CaseFold}, nil
	case len(p.ExpectedContainsAll) > 0:
		return ContainsAll{Keywords: p.ExpectedContainsAll, CaseFold: p.CaseFold}, nil
	case len(p.ExpectedKeys) > 0:
		return JSONHasKeys{RequiredKeys: p.ExpectedKeys}, nil
	case p.ExpectedNumber != nil:
		return Numeric{Expected: *p.ExpectedNumber, Tolerance: p.Tolerance}, nil
	}
	return nil, fmt.Errorf("problem %q: unreachable scorer branch", p.ID)
}

// LiveWorkload is a YAML-authored set of benchmark problems. Distinct from
// Workload (simulation-only) so the benchmark harness can evolve its schema
// without breaking simulation consumers.
type LiveWorkload struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Baseline    string    `yaml:"baseline"`
	Problems    []Problem `yaml:"problems"`
}

// LoadLiveWorkload reads a YAML workload from disk. All scorers are validated
// up front so a bad row is caught before any API calls happen.
func LoadLiveWorkload(path string) (LiveWorkload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LiveWorkload{}, fmt.Errorf("read workload: %w", err)
	}
	var w LiveWorkload
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&w); err != nil {
		return LiveWorkload{}, fmt.Errorf("parse workload: %w", err)
	}
	if w.Name == "" {
		return LiveWorkload{}, fmt.Errorf("workload %q: missing name", path)
	}
	if w.Baseline == "" {
		return LiveWorkload{}, fmt.Errorf("workload %q: missing baseline model", path)
	}
	if len(w.Problems) == 0 {
		return LiveWorkload{}, fmt.Errorf("workload %q: no problems", path)
	}
	seen := map[string]bool{}
	for i, p := range w.Problems {
		if p.ID == "" {
			return LiveWorkload{}, fmt.Errorf("workload %q: problem %d missing id", path, i)
		}
		if seen[p.ID] {
			return LiveWorkload{}, fmt.Errorf("workload %q: duplicate problem id %q", path, p.ID)
		}
		seen[p.ID] = true
		if _, err := p.Scorer(); err != nil {
			return LiveWorkload{}, fmt.Errorf("workload %q: %w", path, err)
		}
	}
	return w, nil
}
