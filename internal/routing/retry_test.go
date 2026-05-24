package routing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoWithRetry_StopsOnSuccess(t *testing.T) {
	calls := 0
	err := DoWithRetry(context.Background(), 3, []time.Duration{time.Microsecond, time.Microsecond}, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("DoWithRetry: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestDoWithRetry_RetriesTransient(t *testing.T) {
	calls := 0
	transient := Transient("x", 503, errors.New("blip"))
	err := DoWithRetry(context.Background(), 3, []time.Duration{time.Microsecond, time.Microsecond}, func() error {
		calls++
		if calls < 3 {
			return transient
		}
		return nil
	})
	if err != nil {
		t.Fatalf("DoWithRetry: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestDoWithRetry_StopsOnPermanent(t *testing.T) {
	calls := 0
	perm := Permanent("x", 401, errors.New("bad key"))
	err := DoWithRetry(context.Background(), 5, []time.Duration{time.Microsecond}, func() error {
		calls++
		return perm
	})
	if !errors.Is(err, perm) {
		t.Fatalf("got %v, want permanent error", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retries on permanent)", calls)
	}
}

func TestDoWithRetry_ContextCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := DoWithRetry(ctx, 5, []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}, func() error {
		calls++
		return Transient("x", 503, errors.New("blip"))
	})
	if err == nil {
		t.Fatalf("expected error when context already canceled")
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0 (pre-canceled ctx should skip attempts)", calls)
	}
}

func TestDoWithRetry_ContextCanceledAfterFirstAttemptStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := DoWithRetry(ctx, 5, []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}, func() error {
		calls++
		cancel()
		return Transient("x", 503, errors.New("blip"))
	})
	if err == nil {
		t.Fatalf("expected error when context canceled during retry loop")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (ctx cancel after first attempt should stop retries)", calls)
	}
}

func TestDoWithRetry_ZeroAttemptsBecomesOne(t *testing.T) {
	calls := 0
	_ = DoWithRetry(context.Background(), 0, nil, func() error {
		calls++
		return Transient("x", 503, errors.New("blip"))
	})
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (attempts < 1 should clamp to 1)", calls)
	}
}
