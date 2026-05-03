// static.go — single-file static asset helper for routes that have
// already done their own routing/auth and just want to hand back
// bytes from disk with a sensible Content-Type.
//
// Pairs naturally with proxy() for SPA setups:
//
//	get /* {
//	  if (env("MX_DEV") == "1") { return proxy("http://localhost:5173", request) }
//	  let f = static_file("./web/dist" + request.path)
//	  if (f != null) { return f }
//	  return html(read_file("./web/dist/index.html"))   // SPA fallback
//	}
//
// Returns null when the path is missing or points at a directory so
// the caller can do its own 404 / fallthrough handling.
package interpreter

import (
	"mime"
	"os"
	"path/filepath"
	"strings"
)

func builtinStaticFile(_ *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	// Refuse any traversal that would escape the working directory.
	// The caller is expected to prefix with their dist/public dir, so
	// "../../etc/passwd" via a hostile request.path must stay inside.
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return NullValue(), nil
	}
	info, err := os.Stat(clean)
	if err != nil || info.IsDir() {
		return NullValue(), nil
	}
	raw, err := os.ReadFile(clean)
	if err != nil {
		return NullValue(), nil
	}
	ct := mime.TypeByExtension(filepath.Ext(clean))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return ResponseValue(&Response{
		Status:      200,
		ContentType: ct,
		Body:        StringValue(string(raw)),
	}), nil
}
