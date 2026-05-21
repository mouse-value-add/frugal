package routing

import "time"

// AttemptHook is called once per provider attempt by a capability's
// fallback router. It lets callers (typically the metrics layer) observe
// every attempt — not just the winner — so error-path latency and
// per-provider error counts stay visible. Pass nil to skip.
//
// Signature is capability-neutral: provider name, latency, USD cost,
// error (nil on success). Search / extract / browse routers all use
// this single shape.
type AttemptHook func(provider string, latency time.Duration, costUSD float64, err error)
