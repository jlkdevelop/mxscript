// metrics.go — minimal Prometheus-compatible metrics. Three primitives:
//
//   metrics.counter(name, value?, labels?)    monotonically increasing
//   metrics.gauge(name, value, labels?)       point-in-time number
//   metrics.histogram(name, value, labels?)   distribution of observations
//
// Plus a built-in /metrics route handler (`metrics.handler()`) that emits
// the standard Prometheus text exposition format. Drop-in for any
// scrape-based monitoring stack — Prometheus, Grafana Cloud, VictoriaMetrics,
// Datadog Agent's openmetrics check, Honeycomb's OTel collector, etc.
//
// Storage is in-process and lives on a single global registry so any
// goroutine (route handler, spawn block, cron job) can record without
// passing a context object around. We use sync.Mutex on writes, atomic
// for counter increments under a lock so reads see consistent state.
package interpreter

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// histogramBuckets defines the upper bounds for the default histogram.
// Values are seconds-flavoured (0.001..10) which fits API latency well.
// Users who need different shapes can call metrics.histogram_buckets to
// override per-name (deferred to a future release).
var histogramBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// metric is one named time series — counter / gauge / histogram. We
// keep the type tag inline so the registry can iterate without a type
// switch on the value side.
type metric struct {
	kind   string // "counter" | "gauge" | "histogram"
	help   string
	values map[string]*metricValue // label-set fingerprint -> value
}

type metricValue struct {
	labels    map[string]string
	counter   float64    // counter / gauge
	histCount uint64     // histogram observation count
	histSum   float64    // sum of all observations
	histBkt   []uint64   // bucket counts, len(histogramBuckets) + 1
}

// registry holds every metric registered in this process. The mutex
// guards both adds and reads — Prometheus scrape happens infrequently
// enough that lock contention isn't a real concern.
type registry struct {
	mu      sync.Mutex
	metrics map[string]*metric
}

var globalRegistry = &registry{metrics: map[string]*metric{}}

// fingerprintLabels turns a label map into a stable key for the
// per-time-series storage. Sorted iteration so {a:1,b:2} and {b:2,a:1}
// share storage.
func fingerprintLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for k, name := range keys {
		if k > 0 {
			b.WriteByte(',')
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(labels[name])
	}
	return b.String()
}

// extractLabels pulls the labels-object argument from an MX call.
// Returns an empty map (not nil) so downstream code can range freely.
func extractLabels(arg Value) map[string]string {
	out := map[string]string{}
	if arg.Kind != KindObject {
		return out
	}
	for _, k := range arg.Object.Keys {
		v, _ := arg.Object.Get(k)
		out[k] = v.Display()
	}
	return out
}

func (r *registry) record(kind, name string, value float64, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.metrics[name]
	if !ok {
		m = &metric{kind: kind, values: map[string]*metricValue{}}
		r.metrics[name] = m
	} else if m.kind != kind {
		// Reusing a name across types is a programmer error — leave
		// the original kind intact so /metrics output stays valid.
		return
	}
	fp := fingerprintLabels(labels)
	mv, ok := m.values[fp]
	if !ok {
		mv = &metricValue{labels: copyLabels(labels)}
		if kind == "histogram" {
			mv.histBkt = make([]uint64, len(histogramBuckets)+1)
		}
		m.values[fp] = mv
	}
	switch kind {
	case "counter":
		mv.counter += value
	case "gauge":
		mv.counter = value
	case "histogram":
		mv.histCount++
		mv.histSum += value
		idx := len(histogramBuckets) // +Inf bucket
		for k, bound := range histogramBuckets {
			if value <= bound {
				idx = k
				break
			}
		}
		mv.histBkt[idx]++
	}
}

func copyLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// expositionString renders the registry into Prometheus' text format:
//
//	# HELP <name> <help>
//	# TYPE <name> <kind>
//	<name>{label="value"} <number>
//
// We sort metric names + label fingerprints so output is byte-stable
// across calls (handy in tests + diff-friendly for ops).
func (r *registry) expositionString() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.metrics))
	for n := range r.metrics {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		m := r.metrics[name]
		if m.help != "" {
			fmt.Fprintf(&b, "# HELP %s %s\n", name, m.help)
		}
		fmt.Fprintf(&b, "# TYPE %s %s\n", name, m.kind)

		fps := make([]string, 0, len(m.values))
		for fp := range m.values {
			fps = append(fps, fp)
		}
		sort.Strings(fps)

		for _, fp := range fps {
			mv := m.values[fp]
			labelStr := renderLabels(mv.labels)
			switch m.kind {
			case "counter", "gauge":
				fmt.Fprintf(&b, "%s%s %g\n", name, labelStr, mv.counter)
			case "histogram":
				cum := uint64(0)
				for k, bound := range histogramBuckets {
					cum += mv.histBkt[k]
					ext := mergeLabel(mv.labels, "le", trimZero(bound))
					fmt.Fprintf(&b, "%s_bucket%s %d\n", name, renderLabels(ext), cum)
				}
				cum += mv.histBkt[len(histogramBuckets)]
				ext := mergeLabel(mv.labels, "le", "+Inf")
				fmt.Fprintf(&b, "%s_bucket%s %d\n", name, renderLabels(ext), cum)
				fmt.Fprintf(&b, "%s_sum%s %g\n", name, labelStr, mv.histSum)
				fmt.Fprintf(&b, "%s_count%s %d\n", name, labelStr, mv.histCount)
			}
		}
	}
	return b.String()
}

func mergeLabel(in map[string]string, k, v string) map[string]string {
	out := copyLabels(in)
	out[k] = v
	return out
}

func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for k, name := range keys {
		if k > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s="%s"`, name, escapeLabelValue(labels[name]))
	}
	b.WriteByte('}')
	return b.String()
}

func escapeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

func trimZero(f float64) string {
	s := fmt.Sprintf("%g", f)
	return s
}

// ===== Builtin shims =====

// metrics.counter(name, value?, labels?) — increment a counter by
// `value` (defaults to 1).
func builtinMetricsCounter(i *Interpreter, args []Value) (Value, error) {
	name, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	value := 1.0
	if len(args) > 1 && args[1].Kind == KindNumber {
		value = args[1].Number
	}
	labels := map[string]string{}
	if len(args) > 2 {
		labels = extractLabels(args[2])
	} else if len(args) > 1 && args[1].Kind == KindObject {
		// Two-arg form: counter(name, labels). Common when the value
		// stays at 1.
		labels = extractLabels(args[1])
		value = 1
	}
	globalRegistry.record("counter", name, value, labels)
	return NullValue(), nil
}

// metrics.gauge(name, value, labels?) — set a gauge.
func builtinMetricsGauge(i *Interpreter, args []Value) (Value, error) {
	name, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 2 || args[1].Kind != KindNumber {
		return Value{}, fmt.Errorf("metrics.gauge(name, value, labels?) requires (string, number)")
	}
	labels := map[string]string{}
	if len(args) > 2 {
		labels = extractLabels(args[2])
	}
	globalRegistry.record("gauge", name, args[1].Number, labels)
	return NullValue(), nil
}

// metrics.histogram(name, value, labels?) — record one observation.
// Bucket bounds are fixed (latency-flavoured) for the MVP; per-metric
// buckets can land in a follow-up.
func builtinMetricsHistogram(i *Interpreter, args []Value) (Value, error) {
	name, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 2 || args[1].Kind != KindNumber {
		return Value{}, fmt.Errorf("metrics.histogram(name, value, labels?) requires (string, number)")
	}
	labels := map[string]string{}
	if len(args) > 2 {
		labels = extractLabels(args[2])
	}
	globalRegistry.record("histogram", name, args[1].Number, labels)
	return NullValue(), nil
}

// metrics.text() returns the current registry rendered as a Prometheus
// exposition string. Most users mount it via metrics.handler() instead,
// but this is exposed for unit tests + custom routes.
func builtinMetricsText(i *Interpreter, args []Value) (Value, error) {
	return StringValue(globalRegistry.expositionString()), nil
}

// metrics.handler() returns a Response object suitable for direct return
// from a route. Equivalent to:
//
//	get /metrics { return metrics.handler() }
func builtinMetricsHandler(i *Interpreter, args []Value) (Value, error) {
	return ResponseValue(&Response{
		ContentType: "text/plain; version=0.0.4; charset=utf-8",
		Body:        StringValue(globalRegistry.expositionString()),
	}), nil
}

// metrics.reset() clears every metric. Test-only — never call this in
// a route handler unless you also rely on a sidecar exporter taking
// snapshots between calls.
func builtinMetricsReset(i *Interpreter, args []Value) (Value, error) {
	globalRegistry.mu.Lock()
	globalRegistry.metrics = map[string]*metric{}
	globalRegistry.mu.Unlock()
	return NullValue(), nil
}
