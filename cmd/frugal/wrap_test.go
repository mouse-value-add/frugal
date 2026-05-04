package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestInjectEnv_OverridesExistingBaseURLsWithoutDuplicates(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"OPENAI_BASE_URL=http://old.local/v1",
		"OPENAI_API_BASE=http://old.local/v1",
		"OPENAI_API_KEY=sk-user-real-key",
	}

	got := injectEnv(env, "http://127.0.0.1:8080/v1", "frugal-session-token")

	if countEnvKey(got, "OPENAI_BASE_URL") != 1 {
		t.Fatalf("expected exactly one OPENAI_BASE_URL entry, got %d", countEnvKey(got, "OPENAI_BASE_URL"))
	}
	if countEnvKey(got, "OPENAI_API_BASE") != 1 {
		t.Fatalf("expected exactly one OPENAI_API_BASE entry, got %d", countEnvKey(got, "OPENAI_API_BASE"))
	}
	if countEnvKey(got, "OPENAI_API_KEY") != 1 {
		t.Fatalf("expected exactly one OPENAI_API_KEY entry, got %d", countEnvKey(got, "OPENAI_API_KEY"))
	}

	if valueForEnvKey(got, "OPENAI_BASE_URL") != "http://127.0.0.1:8080/v1" {
		t.Fatalf("OPENAI_BASE_URL not updated, got %q", valueForEnvKey(got, "OPENAI_BASE_URL"))
	}
	if valueForEnvKey(got, "OPENAI_API_BASE") != "http://127.0.0.1:8080/v1" {
		t.Fatalf("OPENAI_API_BASE not updated, got %q", valueForEnvKey(got, "OPENAI_API_BASE"))
	}
	if valueForEnvKey(got, "OPENAI_API_KEY") != "frugal-session-token" {
		t.Fatalf("OPENAI_API_KEY not replaced with session token, got %q", valueForEnvKey(got, "OPENAI_API_KEY"))
	}
}

func TestInjectEnv_AddsBaseURLsWhenMissing(t *testing.T) {
	env := []string{"PATH=/usr/bin"}
	got := injectEnv(env, "http://127.0.0.1:9090/v1", "")

	if valueForEnvKey(got, "OPENAI_BASE_URL") != "http://127.0.0.1:9090/v1" {
		t.Fatalf("OPENAI_BASE_URL missing or incorrect")
	}
	if valueForEnvKey(got, "OPENAI_API_BASE") != "http://127.0.0.1:9090/v1" {
		t.Fatalf("OPENAI_API_BASE missing or incorrect")
	}
	// Empty auth token means we must not inject a phantom OPENAI_API_KEY
	// when the caller supplied one; callers in production always supply one.
	if valueForEnvKey(got, "OPENAI_API_KEY") != "" {
		t.Fatalf("OPENAI_API_KEY should not be set when authToken is empty")
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

func TestNewWrapHTTPServer_DefaultsAndEnvOverrides(t *testing.T) {
	t.Setenv("FRUGAL_READ_HEADER_TIMEOUT", "")
	t.Setenv("FRUGAL_READ_TIMEOUT", "")
	t.Setenv("FRUGAL_WRITE_TIMEOUT", "")
	t.Setenv("FRUGAL_IDLE_TIMEOUT", "")
	t.Setenv("FRUGAL_MAX_HEADER_BYTES", "")

	srv := newWrapHTTPServer(http.NewServeMux())
	if srv.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected default read header timeout 5s, got %s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 15*time.Second {
		t.Fatalf("expected default read timeout 15s, got %s", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 120*time.Second {
		t.Fatalf("expected default write timeout 120s, got %s", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 60*time.Second {
		t.Fatalf("expected default idle timeout 60s, got %s", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != http.DefaultMaxHeaderBytes {
		t.Fatalf("expected default max header bytes %d, got %d", http.DefaultMaxHeaderBytes, srv.MaxHeaderBytes)
	}

	t.Setenv("FRUGAL_READ_HEADER_TIMEOUT", "7s")
	t.Setenv("FRUGAL_READ_TIMEOUT", "20s")
	t.Setenv("FRUGAL_WRITE_TIMEOUT", "150s")
	t.Setenv("FRUGAL_IDLE_TIMEOUT", "75s")
	t.Setenv("FRUGAL_MAX_HEADER_BYTES", "65536")

	srv = newWrapHTTPServer(http.NewServeMux())
	if srv.ReadHeaderTimeout != 7*time.Second {
		t.Fatalf("expected read header timeout 7s, got %s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 20*time.Second {
		t.Fatalf("expected read timeout 20s, got %s", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 150*time.Second {
		t.Fatalf("expected write timeout 150s, got %s", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 75*time.Second {
		t.Fatalf("expected idle timeout 75s, got %s", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 65536 {
		t.Fatalf("expected max header bytes 65536, got %d", srv.MaxHeaderBytes)
	}
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
