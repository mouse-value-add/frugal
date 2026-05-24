package mcp

import (
	"testing"
	"time"
)

func TestDefaultDuration(t *testing.T) {
	if got := defaultDuration(5*time.Second, 30*time.Second); got != 5*time.Second {
		t.Fatalf("defaultDuration explicit = %v, want 5s", got)
	}
	if got := defaultDuration(0, 30*time.Second); got != 30*time.Second {
		t.Fatalf("defaultDuration zero = %v, want 30s", got)
	}
}
