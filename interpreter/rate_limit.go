// rate_limit.go — application-level rate limiting via the
// `rate_limit(key, max, window_seconds)` builtin. Same token-bucket
// algorithm as the existing server-level rate_limit config, but keyed
// by an arbitrary string so callers can rate-limit per user, per
// tenant, per IP, per endpoint — whatever makes sense for the route.
//
//   route POST /signup {
//     if (!rate_limit("signup:" + request.ip, 5, 60)) {
//       return status(429, { error: "too many requests" })
//     }
//     // ... actual signup
//   }
//
// Buckets live in-process; for distributed rate limiting back this
// onto Redis with `redis.incr(...)` instead.
package interpreter

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// appRateLimit holds the global registry of buckets, one per key.
// We don't expose this directly — callers use the builtin which
// resolves the registry by name + key.
var appRateLimit = struct {
	mu      sync.Mutex
	buckets map[string]*appBucket
}{buckets: map[string]*appBucket{}}

type appBucket struct {
	tokens   float64
	capacity float64
	rate     float64 // tokens per second refill
	last     time.Time
}

// rate_limit(key, max, window_seconds) returns true when the call is
// allowed, false when the key has exhausted its budget. The bucket
// refills linearly: every second the bucket gains `max / window`
// tokens (capped at `max`). The first call after a long pause sees a
// full bucket.
func builtinRateLimit(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 3 {
		return Value{}, fmt.Errorf("rate_limit(key, max, window_seconds) requires 3 args")
	}
	if args[0].Kind != KindString {
		return Value{}, fmt.Errorf("rate_limit: key must be a string")
	}
	if args[1].Kind != KindNumber || args[2].Kind != KindNumber {
		return Value{}, fmt.Errorf("rate_limit: max and window_seconds must be numbers")
	}
	key := args[0].String
	max := args[1].Number
	window := args[2].Number
	if max <= 0 || window <= 0 {
		return BoolValue(false), nil
	}

	appRateLimit.mu.Lock()
	defer appRateLimit.mu.Unlock()
	now := time.Now()
	b, ok := appRateLimit.buckets[key]
	if !ok {
		b = &appBucket{
			tokens:   max,
			capacity: max,
			rate:     max / window,
			last:     now,
		}
		appRateLimit.buckets[key] = b
	} else {
		// Refill the bucket based on elapsed time.
		elapsed := now.Sub(b.last).Seconds()
		b.tokens = math.Min(b.capacity, b.tokens+elapsed*b.rate)
		b.last = now
		// If the user changed the budget shape (e.g., a config reload
		// raised the limit), update capacity/rate and clamp tokens.
		if b.capacity != max {
			b.capacity = max
			b.rate = max / window
			if b.tokens > b.capacity {
				b.tokens = b.capacity
			}
		}
	}
	if b.tokens >= 1 {
		b.tokens--
		return BoolValue(true), nil
	}
	return BoolValue(false), nil
}

// rate_limit_reset(key?) — clears one key (or all if key is omitted).
// Test-only — calling this in production resets the entire fleet's
// rate-limit memory.
func builtinRateLimitReset(_ *Interpreter, args []Value) (Value, error) {
	appRateLimit.mu.Lock()
	defer appRateLimit.mu.Unlock()
	if len(args) > 0 && args[0].Kind == KindString {
		delete(appRateLimit.buckets, args[0].String)
	} else {
		appRateLimit.buckets = map[string]*appBucket{}
	}
	return NullValue(), nil
}
