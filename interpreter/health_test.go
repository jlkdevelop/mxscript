package interpreter

import "testing"

func TestHealthLiveReturns200(t *testing.T) {
	v, err := builtinHealthLive(nil, nil)
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if v.Kind != KindResponse {
		t.Fatalf("got %v", v.Kind)
	}
	if v.Response.Status != 200 {
		t.Errorf("status: got %d, want 200", v.Response.Status)
	}
	body := v.Response.Body
	if body.Kind != KindObject {
		t.Fatalf("body: %+v", body)
	}
	if status, _ := body.Object.Get("status"); status.String != "ok" {
		t.Errorf("status field: got %v", status)
	}
}

func TestHealthReadyAllPass(t *testing.T) {
	i := New()
	checks := NewOrderedMap()
	checks.Set("db", FunctionValue(&Function{Name: "ok", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return BoolValue(true), nil
	}}))
	checks.Set("cache", FunctionValue(&Function{Name: "ok", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return StringValue("PONG"), nil
	}}))
	v, err := builtinHealthReady(i, []Value{ObjectValue(checks)})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if v.Response.Status != 200 {
		t.Errorf("status: got %d, want 200", v.Response.Status)
	}
	body := v.Response.Body.Object
	results, _ := body.Get("checks")
	dbStatus, _ := results.Object.Get("db")
	if dbStatus.String != "ok" {
		t.Errorf("db: got %v", dbStatus)
	}
	cacheStatus, _ := results.Object.Get("cache")
	if cacheStatus.String != "ok" {
		t.Errorf("cache: got %v", cacheStatus)
	}
}

func TestHealthReadyDegradedReturns503(t *testing.T) {
	i := New()
	checks := NewOrderedMap()
	checks.Set("db", FunctionValue(&Function{Name: "ok", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return BoolValue(true), nil
	}}))
	checks.Set("cache", FunctionValue(&Function{Name: "fail", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return BoolValue(false), nil
	}}))
	v, _ := builtinHealthReady(i, []Value{ObjectValue(checks)})
	if v.Response.Status != 503 {
		t.Errorf("status: got %d, want 503", v.Response.Status)
	}
	body := v.Response.Body.Object
	if status, _ := body.Get("status"); status.String != "degraded" {
		t.Errorf("status: got %v, want degraded", status)
	}
	results, _ := body.Get("checks")
	if cache, _ := results.Object.Get("cache"); cache.String != "fail" {
		t.Errorf("cache check: got %v", cache)
	}
}

func TestHealthReadyHandlesThrowingFn(t *testing.T) {
	i := New()
	checks := NewOrderedMap()
	checks.Set("flaky", FunctionValue(&Function{Name: "boom", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return Value{}, &MXError{Message: "connection refused"}
	}}))
	v, _ := builtinHealthReady(i, []Value{ObjectValue(checks)})
	if v.Response.Status != 503 {
		t.Errorf("throwing check should fail readiness, got %d", v.Response.Status)
	}
	body := v.Response.Body.Object
	results, _ := body.Get("checks")
	flaky, _ := results.Object.Get("flaky")
	if flaky.Kind != KindString {
		t.Fatalf("flaky: %+v", flaky)
	}
	// The error message should be embedded for debugging.
	if !contains2(flaky.String, "connection refused") {
		t.Errorf("expected error embedded, got %q", flaky.String)
	}
}

func contains2(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
