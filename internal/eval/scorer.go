package eval

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Scorer judges whether a model response to a benchmark problem is correct.
// Implementations are cheap: exact/substring/numeric/JSON matches run locally
// without spending an LLM-judge call. An LLM-judge scorer can be added later
// as a drop-in implementation.
type Scorer interface {
	Name() string
	Score(output string) ScoreResult
}

// ScoreResult is what a scorer returns. Detail is rendered in verbose reports
// so failures are debuggable without re-running the bench.
type ScoreResult struct {
	Pass   bool
	Detail string
}

// ExactTrimmed matches the output against Expected after trimming whitespace
// from both sides. Case-sensitive. Use for classification labels and
// one-answer math.
type ExactTrimmed struct{ Expected string }

func (s ExactTrimmed) Name() string { return "exact_trimmed" }
func (s ExactTrimmed) Score(out string) ScoreResult {
	got := strings.TrimSpace(out)
	if got == s.Expected {
		return ScoreResult{Pass: true}
	}
	return ScoreResult{Pass: false, Detail: fmt.Sprintf("want %q, got %q", s.Expected, got)}
}

// Substring passes when Expected appears anywhere in the output. CaseFold
// lowercases both sides before comparing — use it for fact-recall where the
// model's answer may be rephrased but must contain the key term.
type Substring struct {
	Expected string
	CaseFold bool
}

func (s Substring) Name() string { return "substring" }
func (s Substring) Score(out string) ScoreResult {
	haystack, needle := out, s.Expected
	if s.CaseFold {
		haystack = strings.ToLower(haystack)
		needle = strings.ToLower(needle)
	}
	if strings.Contains(haystack, needle) {
		return ScoreResult{Pass: true}
	}
	return ScoreResult{Pass: false, Detail: fmt.Sprintf("missing %q", s.Expected)}
}

// ContainsAll passes when every keyword appears in the output (CaseFold
// applies to all). Use for explanations that must hit specific technical
// terms — e.g. "quicksort" answer must mention "pivot" and "partition".
type ContainsAll struct {
	Keywords []string
	CaseFold bool
}

func (s ContainsAll) Name() string { return "contains_all" }
func (s ContainsAll) Score(out string) ScoreResult {
	haystack := out
	if s.CaseFold {
		haystack = strings.ToLower(haystack)
	}
	var missing []string
	for _, kw := range s.Keywords {
		needle := kw
		if s.CaseFold {
			needle = strings.ToLower(needle)
		}
		if !strings.Contains(haystack, needle) {
			missing = append(missing, kw)
		}
	}
	if len(missing) == 0 {
		return ScoreResult{Pass: true}
	}
	return ScoreResult{Pass: false, Detail: "missing keywords: " + strings.Join(missing, ", ")}
}

// JSONHasKeys passes when the output parses as a JSON object and contains
// every required key at the top level. Values aren't type-checked here —
// extend with a schema matcher when the benchmark set needs it.
type JSONHasKeys struct{ RequiredKeys []string }

func (s JSONHasKeys) Name() string { return "json_has_keys" }
func (s JSONHasKeys) Score(out string) ScoreResult {
	// Tolerate fenced markdown around the JSON — common LLM output pattern.
	payload := stripJSONFence(out)
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil {
		return ScoreResult{Pass: false, Detail: "not valid JSON: " + err.Error()}
	}
	var missing []string
	for _, k := range s.RequiredKeys {
		if _, ok := obj[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return ScoreResult{Pass: true}
	}
	return ScoreResult{Pass: false, Detail: "missing keys: " + strings.Join(missing, ", ")}
}

// Numeric pulls the first number out of the output and compares it to
// Expected within ±Tolerance. Lets models preface "The answer is 42" or
// "≈ 3.14159" without failing on prose wrapping.
type Numeric struct {
	Expected  float64
	Tolerance float64
}

var numberRe = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

func (s Numeric) Name() string { return "numeric" }
func (s Numeric) Score(out string) ScoreResult {
	m := numberRe.FindString(out)
	if m == "" {
		return ScoreResult{Pass: false, Detail: "no number in output"}
	}
	got, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return ScoreResult{Pass: false, Detail: "parse %q: " + err.Error()}
	}
	diff := got - s.Expected
	if diff < 0 {
		diff = -diff
	}
	if diff <= s.Tolerance {
		return ScoreResult{Pass: true}
	}
	return ScoreResult{Pass: false, Detail: fmt.Sprintf("want %g±%g, got %g", s.Expected, s.Tolerance, got)}
}

// stripJSONFence removes a surrounding ```json … ``` fence if present.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Skip ``` and optional language tag to end of line.
		if nl := strings.Index(s, "\n"); nl > 0 {
			s = s[nl+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
