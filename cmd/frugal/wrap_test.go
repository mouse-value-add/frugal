package main

import (
	"strings"
	"testing"
)

func TestInjectEnv_OverridesExistingBaseURLsWithoutDuplicates(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"OPENAI_BASE_URL=http://old.local/v1",
		"OPENAI_API_BASE=http://old.local/v1",
	}

	got := injectEnv(env, "http://127.0.0.1:8080/v1")

	if countEnvKey(got, "OPENAI_BASE_URL") != 1 {
		t.Fatalf("expected exactly one OPENAI_BASE_URL entry, got %d", countEnvKey(got, "OPENAI_BASE_URL"))
	}
	if countEnvKey(got, "OPENAI_API_BASE") != 1 {
		t.Fatalf("expected exactly one OPENAI_API_BASE entry, got %d", countEnvKey(got, "OPENAI_API_BASE"))
	}

	if valueForEnvKey(got, "OPENAI_BASE_URL") != "http://127.0.0.1:8080/v1" {
		t.Fatalf("OPENAI_BASE_URL not updated, got %q", valueForEnvKey(got, "OPENAI_BASE_URL"))
	}
	if valueForEnvKey(got, "OPENAI_API_BASE") != "http://127.0.0.1:8080/v1" {
		t.Fatalf("OPENAI_API_BASE not updated, got %q", valueForEnvKey(got, "OPENAI_API_BASE"))
	}
}

func TestInjectEnv_AddsBaseURLsWhenMissing(t *testing.T) {
	env := []string{"PATH=/usr/bin"}
	got := injectEnv(env, "http://127.0.0.1:9090/v1")

	if valueForEnvKey(got, "OPENAI_BASE_URL") != "http://127.0.0.1:9090/v1" {
		t.Fatalf("OPENAI_BASE_URL missing or incorrect")
	}
	if valueForEnvKey(got, "OPENAI_API_BASE") != "http://127.0.0.1:9090/v1" {
		t.Fatalf("OPENAI_API_BASE missing or incorrect")
	}
}

func countEnvKey(env []string, key string) int {
	prefix := key + "="
	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			count++
		}
	}
	return count
}

func valueForEnvKey(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}
