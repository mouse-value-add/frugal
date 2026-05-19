package routing

import (
	"context"
	"math/rand"
	"time"
)

// DoWithRetry runs fn up to attempts times. After each transient failure
// it sleeps for the next backoff in the schedule (with up to 30% jitter)
// before retrying. On permanent failure or context cancel it returns
// immediately. On success returns nil.
//
// attempts ≤ 1 disables retries (one call total). Callers pass the schedule
// they want — defaults are short on purpose: providers we hit have their
// own ~20s HTTP timeout, and the router is already prepared to fall back
// to a different provider when all retries are exhausted.
func DoWithRetry(ctx context.Context, attempts int, backoff []time.Duration, fn func() error) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if IsPermanent(err) {
			return err
		}
		if i == attempts-1 {
			break // out of attempts
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jittered(backoff, i)):
		}
	}
	return err
}

// DefaultBackoff is the in-driver retry schedule used by HTTP-bound
// search drivers. 2 retries (3 attempts total) with sub-second waits.
// Tight on purpose: the router will fall back to a different provider
// anyway, so the goal is to absorb brief blips, not to wait out a real
// outage.
var DefaultBackoff = []time.Duration{200 * time.Millisecond, 800 * time.Millisecond}

func jittered(schedule []time.Duration, i int) time.Duration {
	if i < 0 || i >= len(schedule) {
		// Past the schedule — use the last value (or zero if empty).
		if len(schedule) == 0 {
			return 0
		}
		i = len(schedule) - 1
	}
	base := schedule[i]
	if base <= 0 {
		return 0
	}
	jitter := time.Duration(rand.Int63n(int64(base) / 3)) // 0–33%
	return base + jitter
}
