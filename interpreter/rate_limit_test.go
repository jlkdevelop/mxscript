package interpreter

import (
	"testing"
	"time"
)

func TestRateLimitFirstCallsAllowed(t *testing.T) {
	defer builtinRateLimitReset(nil, nil)
	// First 5 calls inside a 5-per-60s budget all succeed.
	for k := 0; k < 5; k++ {
		v, err := builtinRateLimit(nil, []Value{
			StringValue("user:1"), NumberValue(5), NumberValue(60),
		})
		if err != nil {
			t.Fatalf("call %d: %v", k, err)
		}
		if !v.Bool {
			t.Errorf("call %d: expected allow, got deny", k)
		}
	}
	// 6th must be denied.
	v, _ := builtinRateLimit(nil, []Value{
		StringValue("user:1"), NumberValue(5), NumberValue(60),
	})
	if v.Bool {
		t.Errorf("6th call: expected deny, got allow")
	}
}

func TestRateLimitKeysIndependent(t *testing.T) {
	defer builtinRateLimitReset(nil, nil)
	// Burn the budget on user:1; user:2 should still be untouched.
	for k := 0; k < 3; k++ {
		builtinRateLimit(nil, []Value{StringValue("user:1"), NumberValue(3), NumberValue(60)})
	}
	v, _ := builtinRateLimit(nil, []Value{StringValue("user:1"), NumberValue(3), NumberValue(60)})
	if v.Bool {
		t.Error("user:1 should be exhausted")
	}
	v, _ = builtinRateLimit(nil, []Value{StringValue("user:2"), NumberValue(3), NumberValue(60)})
	if !v.Bool {
		t.Error("user:2 should be allowed")
	}
}

func TestRateLimitRefillsOverTime(t *testing.T) {
	defer builtinRateLimitReset(nil, nil)
	// Tiny budget so we can wait long enough to see a refill in test
	// time without slowing things down: 10 per second = 1 token / 100ms.
	for k := 0; k < 10; k++ {
		builtinRateLimit(nil, []Value{StringValue("ratey"), NumberValue(10), NumberValue(1)})
	}
	v, _ := builtinRateLimit(nil, []Value{StringValue("ratey"), NumberValue(10), NumberValue(1)})
	if v.Bool {
		t.Error("11th call should be denied")
	}
	// Wait ~150ms — should refill 1.5 tokens, so the next call allows.
	time.Sleep(150 * time.Millisecond)
	v, _ = builtinRateLimit(nil, []Value{StringValue("ratey"), NumberValue(10), NumberValue(1)})
	if !v.Bool {
		t.Error("after 150ms refill, call should be allowed")
	}
}

func TestRateLimitInvalidBudget(t *testing.T) {
	v, _ := builtinRateLimit(nil, []Value{StringValue("k"), NumberValue(0), NumberValue(60)})
	if v.Bool {
		t.Error("max=0 should always deny")
	}
	v, _ = builtinRateLimit(nil, []Value{StringValue("k"), NumberValue(5), NumberValue(0)})
	if v.Bool {
		t.Error("window=0 should always deny")
	}
}

func TestRateLimitErrorOnBadArgs(t *testing.T) {
	if _, err := builtinRateLimit(nil, []Value{StringValue("k")}); err == nil {
		t.Error("expected error on missing args")
	}
	if _, err := builtinRateLimit(nil, []Value{NumberValue(1), NumberValue(2), NumberValue(3)}); err == nil {
		t.Error("expected error on non-string key")
	}
}

func TestRateLimitResetSingleKey(t *testing.T) {
	for k := 0; k < 3; k++ {
		builtinRateLimit(nil, []Value{StringValue("a"), NumberValue(3), NumberValue(60)})
		builtinRateLimit(nil, []Value{StringValue("b"), NumberValue(3), NumberValue(60)})
	}
	builtinRateLimitReset(nil, []Value{StringValue("a")})
	// "a" gets a fresh budget; "b" stays exhausted.
	v, _ := builtinRateLimit(nil, []Value{StringValue("a"), NumberValue(3), NumberValue(60)})
	if !v.Bool {
		t.Error("a should be reset")
	}
	v, _ = builtinRateLimit(nil, []Value{StringValue("b"), NumberValue(3), NumberValue(60)})
	if v.Bool {
		t.Error("b should still be exhausted")
	}
	defer builtinRateLimitReset(nil, nil)
}
