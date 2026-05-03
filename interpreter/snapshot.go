// snapshot.go — golden-file testing primitive.
//
//	test "rendered email looks right" {
//	  let body = render_email(user, plan)
//	  assert_snapshot("welcome_email", body)
//	}
//
// First run writes the value's pretty repr to
// __snapshots__/<file>.snap.json under the same directory as the source
// file, then passes. Subsequent runs read the file back and fail if the
// rendered value has changed. `mx test -u` (or --update-snapshots)
// flips the interpreter into update mode, which always overwrites.
//
// Snapshots are committed to source control. They're the test — review
// changes the same way you'd review a code change.
package interpreter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func builtinAssertSnapshot(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("assert_snapshot(name, value) requires 2 args")
	}
	name, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	if !validSnapshotName(name) {
		return Value{}, fmt.Errorf("assert_snapshot: name %q must be path-safe (letters, digits, _ -)", name)
	}
	rendered := PrettyDisplay(args[1], false)

	srcFile := i.File()
	if srcFile == "" {
		return Value{}, fmt.Errorf("assert_snapshot: no source file set; call SetFile first")
	}
	dir := filepath.Join(filepath.Dir(srcFile), "__snapshots__")
	base := filepath.Base(srcFile)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	snapPath := filepath.Join(dir, base+".snap.json")

	existing, err := loadSnapshotFile(snapPath)
	if err != nil {
		return Value{}, err
	}

	stored, hadStored := existing[name]
	mode := i.SnapshotMode()

	if mode == "update" || !hadStored {
		existing[name] = rendered
		if err := writeSnapshotFile(snapPath, existing); err != nil {
			return Value{}, err
		}
		return NullValue(), nil
	}

	if stored == rendered {
		return NullValue(), nil
	}

	return Value{}, fmt.Errorf(
		"assert_snapshot %q failed (re-run with `mx test -u` to update)\n  stored:\n%s\n  current:\n%s",
		name,
		indentLines(stored, "    "),
		indentLines(rendered, "    "),
	)
}

func validSnapshotName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == ' ':
		default:
			return false
		}
	}
	return true
}

func loadSnapshotFile(path string) (map[string]string, error) {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("snapshot file %s: %w", path, err)
	}
	return out, nil
}

func writeSnapshotFile(path string, m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = indent + ln
	}
	return strings.Join(lines, "\n")
}
