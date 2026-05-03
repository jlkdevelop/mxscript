package interpreter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTimeParseRFC3339(t *testing.T) {
	v, err := builtinTimeParse(nil, []Value{StringValue("2026-05-03T12:00:00Z")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Kind != KindNumber {
		t.Fatalf("got %v", v)
	}
	want := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC).Unix()
	if int64(v.Number) != want {
		t.Errorf("unix: got %v, want %v", int64(v.Number), want)
	}
}

func TestTimeParseDateOnly(t *testing.T) {
	v, _ := builtinTimeParse(nil, []Value{StringValue("2026-05-03")})
	if v.Kind != KindNumber {
		t.Fatal("expected number for date-only parse")
	}
}

func TestTimeParseRejectsGarbage(t *testing.T) {
	v, err := builtinTimeParse(nil, []Value{StringValue("not a time")})
	if err != nil {
		t.Fatalf("should not throw: %v", err)
	}
	if v.Kind != KindNull {
		t.Errorf("expected null, got %v", v)
	}
}

func TestTimeFormatRoundtrip(t *testing.T) {
	in := "2026-05-03T12:00:00Z"
	parsed, _ := builtinTimeParse(nil, []Value{StringValue(in)})
	formatted, _ := builtinTimeFormat(nil, []Value{parsed})
	if formatted.String != in {
		t.Errorf("roundtrip: got %q, want %q", formatted.String, in)
	}
}

func TestTimeFormatCustomLayout(t *testing.T) {
	parsed, _ := builtinTimeParse(nil, []Value{StringValue("2026-05-03T15:04:05Z")})
	v, _ := builtinTimeFormat(nil, []Value{parsed, StringValue("2006-01-02 15:04")})
	if v.String != "2026-05-03 15:04" {
		t.Errorf("got %q", v.String)
	}
}

func TestTimeAddDuration(t *testing.T) {
	parsed, _ := builtinTimeParse(nil, []Value{StringValue("2026-05-03T12:00:00Z")})
	v, _ := builtinTimeAdd(nil, []Value{parsed, StringValue("24h")})
	formatted, _ := builtinTimeFormat(nil, []Value{v})
	if formatted.String != "2026-05-04T12:00:00Z" {
		t.Errorf("got %q", formatted.String)
	}
}

func TestTimeDiff(t *testing.T) {
	a, _ := builtinTimeParse(nil, []Value{StringValue("2026-05-03T12:00:00Z")})
	b, _ := builtinTimeParse(nil, []Value{StringValue("2026-05-03T12:00:30Z")})
	v, _ := builtinTimeDiff(nil, []Value{a, b})
	if v.Number != 30 {
		t.Errorf("diff: got %v, want 30", v.Number)
	}
}

func TestTimeWeekday(t *testing.T) {
	// 2026-05-03 is a Sunday.
	parsed, _ := builtinTimeParse(nil, []Value{StringValue("2026-05-03T00:00:00Z")})
	v, _ := builtinTimeWeekday(nil, []Value{parsed})
	if v.String != "Sunday" {
		t.Errorf("weekday: got %q, want Sunday", v.String)
	}
}

func TestTimeComponentExtractors(t *testing.T) {
	parsed, _ := builtinTimeParse(nil, []Value{StringValue("2026-05-03T15:04:05Z")})
	cases := []struct {
		fn   func(*Interpreter, []Value) (Value, error)
		want float64
	}{
		{builtinTimeYear, 2026},
		{builtinTimeMonth, 5},
		{builtinTimeDay, 3},
		{builtinTimeHour, 15},
		{builtinTimeMinute, 4},
		{builtinTimeSecond, 5},
	}
	for _, c := range cases {
		v, _ := c.fn(nil, []Value{parsed})
		if v.Number != c.want {
			t.Errorf("got %v, want %v", v.Number, c.want)
		}
	}
}

func TestPathJoinAndSplit(t *testing.T) {
	v, _ := builtinPathJoin(nil, []Value{
		StringValue("/a"), StringValue("b"), StringValue("c.txt"),
	})
	if v.String != "/a/b/c.txt" {
		t.Errorf("join: got %q", v.String)
	}

	dir, _ := builtinPathDir(nil, []Value{StringValue("/a/b/c.txt")})
	if dir.String != "/a/b" {
		t.Errorf("dir: got %q", dir.String)
	}
	base, _ := builtinPathBase(nil, []Value{StringValue("/a/b/c.txt")})
	if base.String != "c.txt" {
		t.Errorf("base: got %q", base.String)
	}
	ext, _ := builtinPathExt(nil, []Value{StringValue("/a/b/c.txt")})
	if ext.String != ".txt" {
		t.Errorf("ext: got %q", ext.String)
	}
}

func TestFSGlobFlat(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.mx", "b.mx", "c.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	v, err := builtinFSGlob(nil, []Value{StringValue(filepath.Join(dir, "*.mx"))})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if v.Kind != KindArray || len(v.Array) != 2 {
		t.Errorf("expected 2 matches, got %v", v)
	}
}

func TestFSGlobRecursive(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	for _, p := range []string{"a/x.mx", "a/b/y.mx", "a/b/z.txt"} {
		os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644)
	}
	v, err := builtinFSGlob(nil, []Value{StringValue(filepath.Join(dir, "**", "*.mx"))})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if v.Kind != KindArray || len(v.Array) != 2 {
		t.Errorf("recursive glob: want 2 .mx files, got %v", v)
	}
	// Sanity-check we got the expected files (any order).
	got := []string{}
	for _, m := range v.Array {
		got = append(got, filepath.Base(m.String))
	}
	if !contains(got, "x.mx") || !contains(got, "y.mx") {
		t.Errorf("expected x.mx + y.mx, got %v", got)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestTimeNowReturnsRecent(t *testing.T) {
	v, _ := builtinTimeNow(nil, nil)
	now := time.Now().Unix()
	if int64(v.Number) < now-5 || int64(v.Number) > now+5 {
		t.Errorf("time.now too far from real now: %v vs %v", int64(v.Number), now)
	}
}

func TestTimeFormatRequiresNumber(t *testing.T) {
	_, err := builtinTimeFormat(nil, []Value{StringValue("not a number")})
	if err == nil || !strings.Contains(err.Error(), "number") {
		t.Errorf("expected number error, got %v", err)
	}
}
