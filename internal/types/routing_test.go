package types

import "testing"

func TestParseQualityThreshold_NormalizedInputs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want QualityThreshold
	}{
		{name: "exact high", in: "high", want: QualityHigh},
		{name: "uppercase high", in: "HIGH", want: QualityHigh},
		{name: "spaced high", in: "  high  ", want: QualityHigh},
		{name: "mixed-case cost", in: "CoSt", want: QualityCost},
		{name: "spaced balanced", in: " balanced ", want: QualityBalanced},
		{name: "unknown defaults to balanced", in: "fast", want: QualityBalanced},
		{name: "empty defaults to balanced", in: "", want: QualityBalanced},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseQualityThreshold(tt.in); got != tt.want {
				t.Fatalf("ParseQualityThreshold(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
