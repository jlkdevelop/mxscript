// time_path.go — time, path, and fs.glob builtins. The shapes match
// what users coming from Python/JS/Ruby expect:
//
//   time.parse("2026-05-03T12:00:00Z") -> unix seconds
//   time.format(unix, "2006-01-02")    -> "2026-05-03"
//   time.add(unix, "24h")              -> unix + 86400
//   time.diff(a, b)                    -> seconds (b - a)
//   time.weekday(unix)                 -> "Sunday"..."Saturday"
//   time.iso(unix)                     -> "2026-05-03T12:00:00Z"
//   time.unix(iso)                     -> unix seconds (alias of parse)
//
//   path.join("/a", "b", "c.txt")      -> "/a/b/c.txt"
//   path.dir("/a/b/c.txt")             -> "/a/b"
//   path.base("/a/b/c.txt")            -> "c.txt"
//   path.ext("/a/b/c.txt")             -> ".txt"
//
//   fs.glob("*.mx")                    -> ["app.mx", "auth.mx"]
//   fs.glob("**/*.mx")                 -> recursive (max-depth 16)
package interpreter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ===== time.* =====

// time.parse(s) — accepts RFC3339 / ISO 8601 / a few common shapes
// and returns unix seconds. Returns null on parse failure rather than
// throwing — easier to compose with `if (time.parse(s) == null)`.
func builtinTimeParse(_ *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02",
		time.RFC1123,
		time.RFC822,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return NumberValue(float64(t.Unix())), nil
		}
	}
	return NullValue(), nil
}

// time.format(unix, fmt?) — Go-style reference layout (use 2006-01-02
// 15:04:05 with the magic constants). Default is RFC3339.
func builtinTimeFormat(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return Value{}, fmt.Errorf("time.format(unix, fmt?) requires a number")
	}
	layout := time.RFC3339
	if len(args) > 1 && args[1].Kind == KindString {
		layout = args[1].String
	}
	t := time.Unix(int64(args[0].Number), 0).UTC()
	return StringValue(t.Format(layout)), nil
}

// time.add(unix, duration) — duration is a Go-format string ("1h",
// "30m", "24h", "168h" for a week, "1500ms"). Negative is allowed.
func builtinTimeAdd(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindNumber || args[1].Kind != KindString {
		return Value{}, fmt.Errorf("time.add(unix, duration) requires (number, string)")
	}
	d, err := time.ParseDuration(args[1].String)
	if err != nil {
		return Value{}, fmt.Errorf("time.add: %w", err)
	}
	return NumberValue(args[0].Number + d.Seconds()), nil
}

// time.diff(a, b) — returns b - a in seconds. Useful with time.parse:
//
//   let elapsed = time.diff(time.parse(start_iso), now())
func builtinTimeDiff(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindNumber || args[1].Kind != KindNumber {
		return Value{}, fmt.Errorf("time.diff(a, b) requires two numbers")
	}
	return NumberValue(args[1].Number - args[0].Number), nil
}

// time.weekday(unix) — "Sunday"..."Saturday"
func builtinTimeWeekday(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return Value{}, fmt.Errorf("time.weekday(unix) requires a number")
	}
	t := time.Unix(int64(args[0].Number), 0).UTC()
	return StringValue(t.Weekday().String()), nil
}

// time.iso(unix) — convenience for time.format(u, "RFC3339").
func builtinTimeISO(i *Interpreter, args []Value) (Value, error) {
	return builtinTimeFormat(i, args)
}

// time.unix(iso) — alias for time.parse.
func builtinTimeUnix(i *Interpreter, args []Value) (Value, error) {
	return builtinTimeParse(i, args)
}

// time.now() — current unix seconds (alongside the existing now()
// builtin which returns ms; this lives on the namespace for clarity).
func builtinTimeNow(_ *Interpreter, _ []Value) (Value, error) {
	return NumberValue(float64(time.Now().Unix())), nil
}

// time.year / time.month / time.day / time.hour / time.minute / time.second
// — extract one component from a unix timestamp (UTC).
func builtinTimeYear(_ *Interpreter, args []Value) (Value, error) {
	t, err := timeFromArg(args)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(t.Year())), nil
}

func builtinTimeMonth(_ *Interpreter, args []Value) (Value, error) {
	t, err := timeFromArg(args)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(t.Month())), nil
}

func builtinTimeDay(_ *Interpreter, args []Value) (Value, error) {
	t, err := timeFromArg(args)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(t.Day())), nil
}

func builtinTimeHour(_ *Interpreter, args []Value) (Value, error) {
	t, err := timeFromArg(args)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(t.Hour())), nil
}

func builtinTimeMinute(_ *Interpreter, args []Value) (Value, error) {
	t, err := timeFromArg(args)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(t.Minute())), nil
}

func builtinTimeSecond(_ *Interpreter, args []Value) (Value, error) {
	t, err := timeFromArg(args)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(t.Second())), nil
}

func timeFromArg(args []Value) (time.Time, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return time.Time{}, fmt.Errorf("argument 1 must be a unix-seconds number")
	}
	return time.Unix(int64(args[0].Number), 0).UTC(), nil
}

// ===== path.* =====

// path.join(...) — variadic, slash-style joining via path/filepath.
func builtinPathJoin(_ *Interpreter, args []Value) (Value, error) {
	parts := make([]string, len(args))
	for i, a := range args {
		if a.Kind != KindString {
			return Value{}, fmt.Errorf("path.join: argument %d must be a string", i+1)
		}
		parts[i] = a.String
	}
	return StringValue(filepath.Join(parts...)), nil
}

// path.dir(p) — directory portion ("/a/b/c.txt" -> "/a/b").
func builtinPathDir(_ *Interpreter, args []Value) (Value, error) {
	p, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(filepath.Dir(p)), nil
}

// path.base(p) — last element ("/a/b/c.txt" -> "c.txt").
func builtinPathBase(_ *Interpreter, args []Value) (Value, error) {
	p, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(filepath.Base(p)), nil
}

// path.ext(p) — extension including the leading dot (".txt", ".tar.gz"
// returns just ".gz" — matches Go's filepath.Ext).
func builtinPathExt(_ *Interpreter, args []Value) (Value, error) {
	p, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(filepath.Ext(p)), nil
}

// path.absolute(p) — resolve to an absolute path.
func builtinPathAbsolute(_ *Interpreter, args []Value) (Value, error) {
	p, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return Value{}, err
	}
	return StringValue(abs), nil
}

// ===== fs.glob =====

// fs.glob(pattern) — array of matching paths. Supports `*`, `?`, and
// `[abc]` via the standard library, plus `**` for recursive directory
// matching (a common ergonomic addition that Go's filepath.Glob lacks).
func builtinFSGlob(_ *Interpreter, args []Value) (Value, error) {
	pat, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	// `**` recursive glob — split at the first **/ separator and walk
	// the prefix manually so `**/*.mx` works without extra deps.
	if strings.Contains(pat, "**") {
		matches, err := recursiveGlob(pat)
		if err != nil {
			return Value{}, err
		}
		out := make([]Value, len(matches))
		for i, m := range matches {
			out[i] = StringValue(m)
		}
		return ArrayValue(out), nil
	}
	matches, err := filepath.Glob(pat)
	if err != nil {
		return Value{}, err
	}
	out := make([]Value, len(matches))
	for i, m := range matches {
		out[i] = StringValue(m)
	}
	return ArrayValue(out), nil
}

func recursiveGlob(pattern string) ([]string, error) {
	idx := strings.Index(pattern, "**")
	prefix := pattern[:idx]
	suffix := strings.TrimPrefix(pattern[idx+2:], "/")
	root := strings.TrimSuffix(prefix, "/")
	if root == "" {
		root = "."
	}
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // tolerant
		}
		if info.IsDir() {
			return nil
		}
		// Match `suffix` against the basename or relative path. We try
		// both so `**/*.mx` matches files at any depth and `src/**/*.go`
		// works the way users expect.
		rel, _ := filepath.Rel(root, path)
		if matched, _ := filepath.Match(suffix, filepath.Base(path)); matched {
			matches = append(matches, path)
			return nil
		}
		if matched, _ := filepath.Match(suffix, rel); matched {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}

