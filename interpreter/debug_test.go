package interpreter

import (
	"strings"
	"testing"
)

func TestDebugAssertReturnsValueOnTruthy(t *testing.T) {
	v, err := builtinDebugAssert(nil, []Value{StringValue("hello")})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.String != "hello" {
		t.Errorf("got %v, want hello", v)
	}
}

func TestDebugAssertThrowsOnFalsy(t *testing.T) {
	_, err := builtinDebugAssert(nil, []Value{BoolValue(false), StringValue("user logged in")})
	if err == nil || !strings.Contains(err.Error(), "user logged in") {
		t.Errorf("expected message in error, got %v", err)
	}
}

func TestDebugAssertDefaultMessage(t *testing.T) {
	_, err := builtinDebugAssert(nil, []Value{NumberValue(0)})
	if err == nil || !strings.Contains(err.Error(), "assertion failed") {
		t.Errorf("expected default message, got %v", err)
	}
}

func TestDebugInvariantCustomMessage(t *testing.T) {
	_, err := builtinDebugInvariant(nil, []Value{NullValue(), StringValue("must be non-null")})
	if err == nil || !strings.Contains(err.Error(), "invariant violated") {
		t.Errorf("expected invariant in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "must be non-null") {
		t.Errorf("custom message lost: %v", err)
	}
}

func TestDebugUnreachableAlwaysThrows(t *testing.T) {
	_, err := builtinDebugUnreachable(nil, []Value{StringValue("match arm should be exhaustive")})
	if err == nil {
		t.Error("unreachable() should throw")
	}
	if !strings.Contains(err.Error(), "exhaustive") {
		t.Errorf("custom message lost: %v", err)
	}
}

func TestDebugTraceReturnsValue(t *testing.T) {
	i := New()
	fn := &Function{Name: "fast", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return NumberValue(42), nil
	}}
	v, err := builtinDebugTrace(i, []Value{StringValue("fast"), FunctionValue(fn)})
	if err != nil {
		t.Fatalf("trace: %v", err)
	}
	if v.Number != 42 {
		t.Errorf("got %v, want 42", v.Number)
	}
}
