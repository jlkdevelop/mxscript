package interpreter

import (
	"strings"
	"testing"
)

func TestMetricsCounterAccumulates(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	for k := 0; k < 5; k++ {
		_, _ = builtinMetricsCounter(nil, []Value{StringValue("requests_total")})
	}
	out, _ := builtinMetricsText(nil, nil)
	if !strings.Contains(out.String, "requests_total 5") {
		t.Errorf("counter: got %s", out.String)
	}
}

func TestMetricsCounterWithValue(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("bytes"), NumberValue(2048)})
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("bytes"), NumberValue(512)})
	out, _ := builtinMetricsText(nil, nil)
	if !strings.Contains(out.String, "bytes 2560") {
		t.Errorf("counter+value: got %s", out.String)
	}
}

func TestMetricsCounterWithLabels(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	labels := NewOrderedMap()
	labels.Set("method", StringValue("GET"))
	labels.Set("path", StringValue("/users"))
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("http_requests"), NumberValue(1), ObjectValue(labels)})
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("http_requests"), NumberValue(1), ObjectValue(labels)})

	labels2 := NewOrderedMap()
	labels2.Set("method", StringValue("POST"))
	labels2.Set("path", StringValue("/users"))
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("http_requests"), NumberValue(1), ObjectValue(labels2)})

	out, _ := builtinMetricsText(nil, nil)
	if !strings.Contains(out.String, `http_requests{method="GET",path="/users"} 2`) {
		t.Errorf("labeled counter: %s", out.String)
	}
	if !strings.Contains(out.String, `http_requests{method="POST",path="/users"} 1`) {
		t.Errorf("second label set: %s", out.String)
	}
}

func TestMetricsGaugeSetsValue(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	_, _ = builtinMetricsGauge(nil, []Value{StringValue("active_connections"), NumberValue(42)})
	_, _ = builtinMetricsGauge(nil, []Value{StringValue("active_connections"), NumberValue(7)})
	out, _ := builtinMetricsText(nil, nil)
	if !strings.Contains(out.String, "active_connections 7") {
		t.Errorf("gauge: %s", out.String)
	}
}

func TestMetricsHistogramBuckets(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	for _, v := range []float64{0.001, 0.05, 0.05, 1.5, 100} {
		_, _ = builtinMetricsHistogram(nil, []Value{StringValue("latency_seconds"), NumberValue(v)})
	}
	out, _ := builtinMetricsText(nil, nil)
	// Cumulative: bucket le=0.001 should have 1, le=0.05 should have 3
	// (the 1.5 and 100 spill into +Inf).
	if !strings.Contains(out.String, `latency_seconds_bucket{le="0.001"} 1`) {
		t.Errorf("le=0.001 bucket: %s", out.String)
	}
	if !strings.Contains(out.String, `latency_seconds_bucket{le="0.05"} 3`) {
		t.Errorf("le=0.05 bucket: %s", out.String)
	}
	if !strings.Contains(out.String, `latency_seconds_bucket{le="+Inf"} 5`) {
		t.Errorf("+Inf bucket: %s", out.String)
	}
	if !strings.Contains(out.String, "latency_seconds_count 5") {
		t.Errorf("count: %s", out.String)
	}
}

func TestMetricsTextIncludesTypeLine(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("c")})
	_, _ = builtinMetricsGauge(nil, []Value{StringValue("g"), NumberValue(1)})
	_, _ = builtinMetricsHistogram(nil, []Value{StringValue("h"), NumberValue(0.1)})
	out, _ := builtinMetricsText(nil, nil)
	for _, want := range []string{
		"# TYPE c counter",
		"# TYPE g gauge",
		"# TYPE h histogram",
	} {
		if !strings.Contains(out.String, want) {
			t.Errorf("missing %q in:\n%s", want, out.String)
		}
	}
}

func TestMetricsHandlerReturnsResponse(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("ping_total")})
	v, _ := builtinMetricsHandler(nil, nil)
	if v.Kind != KindResponse {
		t.Fatalf("handler: want response, got %v", v.Kind)
	}
	if !strings.Contains(v.Response.ContentType, "text/plain") {
		t.Errorf("content-type: got %q", v.Response.ContentType)
	}
	if v.Response.Body.Kind != KindString || !strings.Contains(v.Response.Body.String, "ping_total") {
		t.Errorf("handler body: %v", v.Response.Body)
	}
}

func TestMetricsLabelOrderingDeterministic(t *testing.T) {
	defer builtinMetricsReset(nil, nil)
	// Two label objects with keys inserted in different orders must
	// share the same series — fingerprint sort needs to be stable.
	a := NewOrderedMap()
	a.Set("a", StringValue("1"))
	a.Set("b", StringValue("2"))
	b := NewOrderedMap()
	b.Set("b", StringValue("2"))
	b.Set("a", StringValue("1"))

	_, _ = builtinMetricsCounter(nil, []Value{StringValue("x"), NumberValue(1), ObjectValue(a)})
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("x"), NumberValue(1), ObjectValue(b)})
	out, _ := builtinMetricsText(nil, nil)
	// Single time series with count 2.
	if strings.Count(out.String, `x{a="1",b="2"}`) != 1 {
		t.Errorf("expected one series, got:\n%s", out.String)
	}
	if !strings.Contains(out.String, `x{a="1",b="2"} 2`) {
		t.Errorf("merged value: %s", out.String)
	}
}

func TestMetricsKindClashIgnored(t *testing.T) {
	// Reusing a name as a different type must not corrupt the
	// original series — programmer-error case, not data error.
	defer builtinMetricsReset(nil, nil)
	_, _ = builtinMetricsCounter(nil, []Value{StringValue("clash")})
	_, _ = builtinMetricsGauge(nil, []Value{StringValue("clash"), NumberValue(99)})
	out, _ := builtinMetricsText(nil, nil)
	if !strings.Contains(out.String, "# TYPE clash counter") {
		t.Errorf("kind should stay counter: %s", out.String)
	}
}
