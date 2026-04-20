package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForReadyReturnsAfterHealthyStatus(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	if err := waitForReady(ts.URL, 500*time.Millisecond); err != nil {
		t.Fatalf("expected readiness check to succeed, got error: %v", err)
	}
}

func TestWaitForReadyTimesOutWhenUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve local port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	url := fmt.Sprintf("http://%s/health", addr)
	if err := waitForReady(url, 150*time.Millisecond); err == nil {
		t.Fatalf("expected timeout error for unreachable endpoint")
	}
}
