// health.go — Kubernetes-flavoured liveness / readiness probes.
//
//	route GET /healthz { return health.live() }
//	route GET /readyz {
//	  return health.ready({
//	    database: fn() { sql.query_one(db, "SELECT 1") != null },
//	    redis:    fn() { redis.get(r, "ping") != null }
//	  })
//	}
//
// The conventions are deliberate: healthz checks "is this process
// alive enough to keep" — nothing else, returns 200 once the binary
// is responding to HTTP. readyz checks "is this process ready to
// serve traffic" — runs every check fn, returns 200 if all pass and
// 503 with a per-check JSON body otherwise.
package interpreter

import "fmt"

// health.live() — returns a 200 OK response. Use as the readyness
// probe target: it succeeds the moment the HTTP server is up.
func builtinHealthLive(_ *Interpreter, _ []Value) (Value, error) {
	out := NewOrderedMap()
	out.Set("status", StringValue("ok"))
	return ResponseValue(&Response{
		Status:      200,
		ContentType: "application/json",
		Body:        ObjectValue(out),
	}), nil
}

// health.ready(checks) — runs every fn in `checks`, returns 200 with
// each result when all pass, 503 if any fail. The body always
// reports per-check status so dashboards can show which dependency
// is down even when the response is degraded.
func builtinHealthReady(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("health.ready(checks) requires an object of fn checks")
	}
	results := NewOrderedMap()
	allOK := true
	for _, name := range args[0].Object.Keys {
		fn, _ := args[0].Object.Get(name)
		if fn.Kind != KindFunction {
			results.Set(name, StringValue("not a function"))
			allOK = false
			continue
		}
		v, err := i.callFunction(nil, fn.Function, nil)
		switch {
		case err != nil:
			results.Set(name, StringValue("error: "+err.Error()))
			allOK = false
		case v.Kind == KindBool && !v.Bool, v.Kind == KindNull:
			results.Set(name, StringValue("fail"))
			allOK = false
		default:
			results.Set(name, StringValue("ok"))
		}
	}
	out := NewOrderedMap()
	if allOK {
		out.Set("status", StringValue("ok"))
	} else {
		out.Set("status", StringValue("degraded"))
	}
	out.Set("checks", ObjectValue(results))
	status := 200
	if !allOK {
		status = 503
	}
	return ResponseValue(&Response{
		Status:      status,
		ContentType: "application/json",
		Body:        ObjectValue(out),
	}), nil
}
