// upload.go — convenience helpers for the multipart upload story.
//
// `request.files` already gives you parsed file objects with name /
// size / content_type / content / ext. The friction was the second
// step: writing them to disk safely. Without save_upload(), users wrote:
//
//	let dir = "./uploads"
//	write_file(dir + "/" + uuid() + img.ext, img.content)
//
// — which has to mkdir the parent themselves, and the call is verbose
// in the hot path of every endpoint that takes uploads. save_upload()
// collapses that.
package interpreter

import (
	"fmt"
	"os"
	"path/filepath"
)

// save_upload(file, path) — write a multipart file to disk.
//
// `file` is the object you got from `request.files.<name>` (or one
// element of the array when a field has multiple files). `path` is
// the destination on disk; parent directories are created if missing.
//
// Returns:
//
//	{ ok: true,  path: "...", size: N }   // success
//	{ ok: false, error: "..." }           // disk error
//
// Atomic: writes to `path.tmp` then renames so a partial write can't
// be observed. 0644 perms.
func builtinSaveUpload(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("save_upload(file, path) requires 2 arguments")
	}
	file := args[0]
	if file.Kind != KindObject {
		return Value{}, fmt.Errorf("save_upload: first arg must be a file object (got %s)", file.typeName())
	}
	path, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}

	contentVal, ok := file.Object.Get("content")
	if !ok || contentVal.Kind != KindString {
		return saveUploadFail("file object missing `content` field"), nil
	}
	content := contentVal.String

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return saveUploadFail(err.Error()), nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return saveUploadFail(err.Error()), nil
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return saveUploadFail(err.Error()), nil
	}

	out := NewOrderedMap()
	out.Set("ok", BoolValue(true))
	out.Set("path", StringValue(path))
	out.Set("size", NumberValue(float64(len(content))))
	return ObjectValue(out), nil
}

func saveUploadFail(msg string) Value {
	out := NewOrderedMap()
	out.Set("ok", BoolValue(false))
	out.Set("error", StringValue(msg))
	return ObjectValue(out)
}
