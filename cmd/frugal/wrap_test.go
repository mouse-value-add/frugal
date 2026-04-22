package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWaitForReadySuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	if err := waitForReady(ts.URL+"/health", 500*time.Millisecond); err != nil {
		t.Fatalf("expected readiness check to succeed, got error: %v", err)
	}
}

func TestWaitForReadyTimeout(t *testing.T) {
	err := waitForReady("http://127.0.0.1:1/health", 75*time.Millisecond)
	if err == nil {
		t.Fatal("expected readiness timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}
