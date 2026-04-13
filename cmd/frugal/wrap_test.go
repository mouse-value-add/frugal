package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWaitForReady_SucceedsOnHealthyEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	if err := waitForReady(ts.URL, 500*time.Millisecond); err != nil {
		t.Fatalf("expected ready endpoint to succeed, got error: %v", err)
	}
}

func TestWaitForReady_TimesOutWhenEndpointUnavailable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // ensure no server is listening

	start := time.Now()
	err = waitForReady(fmt.Sprintf("http://%s/health", addr), 120*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("waitForReady returned too quickly: %v", elapsed)
	}
}
