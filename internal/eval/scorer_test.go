package eval

import "testing"

func TestExactTrimmed(t *testing.T) {
	s := ExactTrimmed{Expected: "42"}
	if !s.Score("  42  ").Pass {
		t.Fatalf("whitespace around exact match should pass")
	}
	if s.Score("42.0").Pass {
		t.Fatalf("42.0 should not exactly equal 42")
	}
	if s.Score("the answer is 42").Pass {
		t.Fatalf("substring should not satisfy exact match")
	}
}

func TestSubstring_CaseFold(t *testing.T) {
	s := Substring{Expected: "Paris", CaseFold: true}
	if !s.Score("the capital is paris, france").Pass {
		t.Fatalf("case-fold substring should match")
	}
	if s.Score("london").Pass {
		t.Fatalf("non-matching substring should fail")
	}
}

func TestContainsAll_ReportsMissing(t *testing.T) {
	s := ContainsAll{Keywords: []string{"pivot", "partition", "divide"}, CaseFold: true}
	r := s.Score("Quicksort partitions around a pivot element.")
	if r.Pass {
		t.Fatalf("expected failure when one keyword missing")
	}
	if r.Detail == "" {
		t.Fatalf("expected detail listing missing keywords")
	}
	r2 := s.Score("Quicksort divides the array by partitioning around a pivot.")
	if !r2.Pass {
		t.Fatalf("expected pass when all keywords present; detail=%s", r2.Detail)
	}
}

func TestJSONHasKeys_ToleratesMarkdownFence(t *testing.T) {
	s := JSONHasKeys{RequiredKeys: []string{"name", "email"}}
	fenced := "```json\n{\"name\":\"Jane\",\"email\":\"j@x.co\"}\n```"
	if !s.Score(fenced).Pass {
		t.Fatalf("fenced JSON should parse and match")
	}
	missing := s.Score(`{"name":"Jane"}`)
	if missing.Pass {
		t.Fatalf("should fail when required key is missing")
	}
	bad := s.Score("not json at all")
	if bad.Pass {
		t.Fatalf("invalid JSON should not pass")
	}
}

func TestNumeric_WithinTolerance(t *testing.T) {
	s := Numeric{Expected: 714, Tolerance: 0}
	if !s.Score("The answer is 714.").Pass {
		t.Fatalf("exact number in prose should pass")
	}
	if s.Score("715").Pass {
		t.Fatalf("off-by-one with zero tolerance should fail")
	}

	pi := Numeric{Expected: 3.14159, Tolerance: 0.001}
	if !pi.Score("pi ≈ 3.1416").Pass {
		t.Fatalf("within-tolerance float should pass")
	}
	if pi.Score("pi ≈ 3.15").Pass {
		t.Fatalf("outside-tolerance float should fail")
	}
}
