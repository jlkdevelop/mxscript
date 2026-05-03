// export.go — high-level response helpers for the most common export
// shapes APIs serve: CSV (for spreadsheets) and NDJSON (for streaming
// pipelines / log shippers).
//
// Pattern:
//
//	get /export/users.csv {
//	  return csv(sql.find(db, "users", {}), { filename: "users.csv" })
//	}
//
//	get /export/events.ndjson {
//	  return ndjson(sql.find(db, "events", { active: true }))
//	}
//
// `filename` opts triggers `Content-Disposition: attachment` so a
// browser downloads instead of trying to render.
package interpreter

import (
	"bytes"
	"encoding/csv"
	"fmt"
)

// csv(items, opts?) -> Response
//
// `items` is an array of row objects (same shape sql.find returns).
// Column order follows the first row's keys for deterministic output.
// opts:
//
//	filename string  — sets Content-Disposition: attachment;filename=
func builtinCSVResponse(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("csv(items, opts?) requires an array of objects")
	}
	body, err := renderCSVRows(args[0].Array)
	if err != nil {
		return Value{}, err
	}
	resp := &Response{
		Status:      200,
		ContentType: "text/csv; charset=utf-8",
		Body:        StringValue(body),
		Headers:     map[string]string{},
	}
	if len(args) > 1 && args[1].Kind == KindObject {
		if v, ok := args[1].Object.Get("filename"); ok && v.Kind == KindString && v.String != "" {
			resp.Headers["Content-Disposition"] = `attachment; filename="` + v.String + `"`
		}
	}
	return ResponseValue(resp), nil
}

// ndjson(items) -> Response — newline-delimited JSON. Each row is
// JSON-encoded on its own line, no trailing newline. Content-Type is
// `application/x-ndjson`, the de-facto standard for streaming JSON.
func builtinNDJSONResponse(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("ndjson(items) requires an array")
	}
	var buf bytes.Buffer
	for i, item := range args[0].Array {
		raw, err := jsonEncode(item)
		if err != nil {
			return Value{}, fmt.Errorf("ndjson: row %d: %w", i, err)
		}
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.Write(raw)
	}
	return ResponseValue(&Response{
		Status:      200,
		ContentType: "application/x-ndjson",
		Body:        StringValue(buf.String()),
	}), nil
}

func renderCSVRows(rows []Value) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}
	if rows[0].Kind != KindObject {
		return "", fmt.Errorf("csv: each row must be an object")
	}
	headers := append([]string(nil), rows[0].Object.Keys...)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(headers); err != nil {
		return "", err
	}
	for i, row := range rows {
		if row.Kind != KindObject {
			return "", fmt.Errorf("csv: row %d must be an object", i)
		}
		fields := make([]string, len(headers))
		for k, h := range headers {
			v, _ := row.Object.Get(h)
			fields[k] = v.Display()
		}
		if err := w.Write(fields); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
