// builtins.go installs the MX Script standard library into the global
// environment. Every native function is registered here so they're
// available in every .mx program without an import.
package interpreter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func registerBuiltins(i *Interpreter) {
	g := i.globals

	def := func(name string, fn func(interp *Interpreter, args []Value) (Value, error)) {
		g.Set(name, FunctionValue(&Function{Name: name, Native: fn}))
	}

	// --- Output ---
	def("print", builtinPrint)
	def("println", builtinPrint)

	// --- HTTP response helpers ---
	def("json", builtinJSON)
	def("text", builtinText)
	def("html", builtinHTML)
	def("status", builtinStatus)
	def("redirect", builtinRedirect)

	// --- Environment / I/O ---
	def("env", builtinEnv)
	def("fetch", builtinFetch)
	def("error", builtinError)
	def("typeof", builtinTypeof)
	def("now", builtinNow)
	def("sleep", builtinSleep)

	// --- String ops ---
	def("len", builtinLen)
	def("upper", builtinUpper)
	def("lower", builtinLower)
	def("split", builtinSplit)
	def("trim", builtinTrim)
	def("contains", builtinContains)
	def("replace", builtinReplace)
	def("starts_with", builtinStartsWith)
	def("ends_with", builtinEndsWith)
	def("str", builtinStr)
	def("num", builtinNum)

	// --- Array ops ---
	def("push", builtinPush)
	def("pop", builtinPop)
	def("map", builtinMap)
	def("filter", builtinFilter)
	def("find", builtinFind)
	def("join", builtinJoin)
	def("reverse", builtinReverse)
	def("range", builtinRange)
	def("keys", builtinKeys)
	def("values", builtinValues)

	// --- Math ---
	def("round", builtinRound)
	def("floor", builtinFloor)
	def("ceil", builtinCeil)
	def("abs", builtinAbs)
	def("min", builtinMin)
	def("max", builtinMax)
	def("random", builtinRandom)

	// --- Type checks ---
	def("isString", builtinIsString)
	def("isNumber", builtinIsNumber)
	def("isBool", builtinIsBool)
	def("isNull", builtinIsNull)
	def("isArray", builtinIsArray)
	def("isObject", builtinIsObject)
	def("isFunction", builtinIsFunction)

	// --- JSON helpers ---
	def("json_parse", builtinJSONParse)
	def("json_stringify", builtinJSONStringify)

	// --- AI namespace ---
	ai := NewOrderedMap()
	ai.Set("complete", FunctionValue(&Function{Name: "ai.complete", Native: builtinAIComplete}))
	ai.Set("embed", FunctionValue(&Function{Name: "ai.embed", Native: builtinAIEmbed}))
	g.Set("ai", ObjectValue(ai))
}

// ===== Output =====

func builtinPrint(i *Interpreter, args []Value) (Value, error) {
	parts := make([]string, len(args))
	for k, a := range args {
		parts[k] = a.Display()
	}
	fmt.Println(strings.Join(parts, " "))
	return NullValue(), nil
}

// ===== HTTP response helpers =====

func builtinJSON(i *Interpreter, args []Value) (Value, error) {
	var body Value = NullValue()
	if len(args) > 0 {
		body = args[0]
	}
	return ResponseValue(&Response{ContentType: "application/json", Body: body}), nil
}

func builtinText(i *Interpreter, args []Value) (Value, error) {
	var body Value = StringValue("")
	if len(args) > 0 {
		body = args[0]
	}
	if body.Kind != KindString {
		body = StringValue(body.Display())
	}
	return ResponseValue(&Response{ContentType: "text/plain; charset=utf-8", Body: body}), nil
}

func builtinHTML(i *Interpreter, args []Value) (Value, error) {
	var body Value = StringValue("")
	if len(args) > 0 {
		body = args[0]
	}
	if body.Kind != KindString {
		body = StringValue(body.Display())
	}
	return ResponseValue(&Response{ContentType: "text/html; charset=utf-8", Body: body}), nil
}

func builtinStatus(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return Value{}, fmt.Errorf("status(code, body?) requires a numeric status code")
	}
	resp := &Response{Status: int(args[0].Number), ContentType: "application/json"}
	if len(args) > 1 {
		resp.Body = args[1]
	}
	return ResponseValue(resp), nil
}

func builtinRedirect(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("redirect(url) requires a string URL")
	}
	status := 302
	if len(args) > 1 && args[1].Kind == KindNumber {
		status = int(args[1].Number)
	}
	return ResponseValue(&Response{
		Status:      status,
		ContentType: "text/plain",
		Body:        StringValue("Redirecting..."),
		Headers:     map[string]string{"Location": args[0].String},
	}), nil
}

// ===== Environment / I/O =====

func builtinEnv(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("env(name) requires a string name")
	}
	v := os.Getenv(args[0].String)
	if v == "" && len(args) > 1 {
		return args[1], nil
	}
	return StringValue(v), nil
}

func builtinFetch(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("fetch(url, opts?) requires a string URL")
	}
	u := args[0].String
	method := "GET"
	var body io.Reader
	headers := map[string]string{}

	if len(args) > 1 && args[1].Kind == KindObject {
		opts := args[1].Object
		if v, ok := opts.Get("method"); ok && v.Kind == KindString {
			method = v.String
		}
		if v, ok := opts.Get("body"); ok {
			if v.Kind == KindString {
				body = strings.NewReader(v.String)
				if _, hasCT := opts.Get("headers"); !hasCT {
					headers["Content-Type"] = "text/plain"
				}
			} else {
				b, err := jsonEncode(v)
				if err != nil {
					return Value{}, err
				}
				body = bytes.NewReader(b)
				headers["Content-Type"] = "application/json"
			}
		}
		if v, ok := opts.Get("headers"); ok && v.Kind == KindObject {
			for _, k := range v.Object.Keys {
				if hv, _ := v.Object.Get(k); hv.Kind == KindString {
					headers[k] = hv.String
				}
			}
		}
	}

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return Value{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	out.Set("status", NumberValue(float64(resp.StatusCode)))
	hdr := NewOrderedMap()
	for k := range resp.Header {
		hdr.Set(strings.ToLower(k), StringValue(resp.Header.Get(k)))
	}
	out.Set("headers", ObjectValue(hdr))

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		if v, err := jsonDecode(raw); err == nil {
			out.Set("body", v)
		} else {
			out.Set("body", StringValue(string(raw)))
		}
	} else {
		out.Set("body", StringValue(string(raw)))
	}
	out.Set("text", StringValue(string(raw)))
	return ObjectValue(out), nil
}

func builtinError(i *Interpreter, args []Value) (Value, error) {
	msg := "error"
	if len(args) > 0 {
		msg = args[0].Display()
	}
	return Value{}, fmt.Errorf("%s", msg)
}

func builtinTypeof(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return StringValue("null"), nil
	}
	return StringValue(args[0].typeName()), nil
}

func builtinNow(i *Interpreter, args []Value) (Value, error) {
	return NumberValue(float64(time.Now().UnixMilli())), nil
}

func builtinSleep(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return Value{}, fmt.Errorf("sleep(ms) requires a number")
	}
	time.Sleep(time.Duration(args[0].Number) * time.Millisecond)
	return NullValue(), nil
}

// ===== String ops =====

func builtinLen(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return Value{}, fmt.Errorf("len(x) requires 1 argument")
	}
	switch args[0].Kind {
	case KindString:
		return NumberValue(float64(len([]rune(args[0].String)))), nil
	case KindArray:
		return NumberValue(float64(len(args[0].Array))), nil
	case KindObject:
		return NumberValue(float64(len(args[0].Object.Keys))), nil
	}
	return Value{}, fmt.Errorf("len() not supported on %s", args[0].typeName())
}

func stringArg(args []Value, i int) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("missing string argument %d", i+1)
	}
	if args[i].Kind != KindString {
		return "", fmt.Errorf("argument %d must be a string", i+1)
	}
	return args[i].String, nil
}

func builtinUpper(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(strings.ToUpper(s)), nil
}

func builtinLower(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(strings.ToLower(s)), nil
}

func builtinSplit(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	sep, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	parts := strings.Split(s, sep)
	out := make([]Value, len(parts))
	for k, p := range parts {
		out[k] = StringValue(p)
	}
	return ArrayValue(out), nil
}

func builtinTrim(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(strings.TrimSpace(s)), nil
}

func builtinContains(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("contains(target, value) requires 2 arguments")
	}
	switch args[0].Kind {
	case KindString:
		if args[1].Kind != KindString {
			return Value{}, fmt.Errorf("contains expects string needle for string haystack")
		}
		return BoolValue(strings.Contains(args[0].String, args[1].String)), nil
	case KindArray:
		for _, el := range args[0].Array {
			if valuesEqual(el, args[1]) {
				return BoolValue(true), nil
			}
		}
		return BoolValue(false), nil
	}
	return Value{}, fmt.Errorf("contains() not supported on %s", args[0].typeName())
}

func builtinReplace(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	old, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	new_, err := stringArg(args, 2)
	if err != nil {
		return Value{}, err
	}
	return StringValue(strings.ReplaceAll(s, old, new_)), nil
}

func builtinStartsWith(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	prefix, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	return BoolValue(strings.HasPrefix(s, prefix)), nil
}

func builtinEndsWith(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	suffix, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	return BoolValue(strings.HasSuffix(s, suffix)), nil
}

func builtinStr(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return StringValue(""), nil
	}
	return StringValue(args[0].Display()), nil
}

func builtinNum(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return NumberValue(0), nil
	}
	switch args[0].Kind {
	case KindNumber:
		return args[0], nil
	case KindString:
		f, err := strconv.ParseFloat(strings.TrimSpace(args[0].String), 64)
		if err != nil {
			return Value{}, fmt.Errorf("cannot convert %q to number", args[0].String)
		}
		return NumberValue(f), nil
	case KindBool:
		if args[0].Bool {
			return NumberValue(1), nil
		}
		return NumberValue(0), nil
	}
	return Value{}, fmt.Errorf("cannot convert %s to number", args[0].typeName())
}

// ===== Array ops =====

func builtinPush(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("push(array, value) requires an array and a value")
	}
	out := append([]Value{}, args[0].Array...)
	out = append(out, args[1:]...)
	return ArrayValue(out), nil
}

func builtinPop(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("pop(array) requires an array")
	}
	a := args[0].Array
	if len(a) == 0 {
		return NullValue(), nil
	}
	return a[len(a)-1], nil
}

func builtinMap(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("map(array, fn) requires an array and a function")
	}
	out := make([]Value, 0, len(args[0].Array))
	for _, el := range args[0].Array {
		v, err := i.callFunction(nil, args[1].Function, []Value{el})
		if err != nil {
			return Value{}, err
		}
		out = append(out, v)
	}
	return ArrayValue(out), nil
}

func builtinFilter(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("filter(array, fn) requires an array and a function")
	}
	out := make([]Value, 0)
	for _, el := range args[0].Array {
		v, err := i.callFunction(nil, args[1].Function, []Value{el})
		if err != nil {
			return Value{}, err
		}
		if v.IsTruthy() {
			out = append(out, el)
		}
	}
	return ArrayValue(out), nil
}

func builtinFind(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("find(array, fn) requires an array and a function")
	}
	for _, el := range args[0].Array {
		v, err := i.callFunction(nil, args[1].Function, []Value{el})
		if err != nil {
			return Value{}, err
		}
		if v.IsTruthy() {
			return el, nil
		}
	}
	return NullValue(), nil
}

func builtinJoin(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("join(array, sep?) requires an array")
	}
	sep := ""
	if len(args) > 1 && args[1].Kind == KindString {
		sep = args[1].String
	}
	parts := make([]string, len(args[0].Array))
	for k, el := range args[0].Array {
		parts[k] = el.Display()
	}
	return StringValue(strings.Join(parts, sep)), nil
}

func builtinReverse(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("reverse(array) requires an array")
	}
	a := args[0].Array
	out := make([]Value, len(a))
	for k := range a {
		out[k] = a[len(a)-1-k]
	}
	return ArrayValue(out), nil
}

func builtinRange(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return Value{}, fmt.Errorf("range(end) or range(start, end) requires numbers")
	}
	start, end := 0.0, args[0].Number
	if len(args) > 1 && args[1].Kind == KindNumber {
		start = args[0].Number
		end = args[1].Number
	}
	out := make([]Value, 0, int(end-start))
	for k := start; k < end; k++ {
		out = append(out, NumberValue(k))
	}
	return ArrayValue(out), nil
}

func builtinKeys(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("keys(object) requires an object")
	}
	out := make([]Value, len(args[0].Object.Keys))
	for k, key := range args[0].Object.Keys {
		out[k] = StringValue(key)
	}
	return ArrayValue(out), nil
}

func builtinValues(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("values(object) requires an object")
	}
	out := make([]Value, len(args[0].Object.Keys))
	for k, key := range args[0].Object.Keys {
		out[k] = args[0].Object.Values[key]
	}
	return ArrayValue(out), nil
}

// ===== Math =====

func numberArg(args []Value, i int) (float64, error) {
	if i >= len(args) {
		return 0, fmt.Errorf("missing number argument %d", i+1)
	}
	if args[i].Kind != KindNumber {
		return 0, fmt.Errorf("argument %d must be a number", i+1)
	}
	return args[i].Number, nil
}

func builtinRound(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Round(n)), nil
}
func builtinFloor(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Floor(n)), nil
}
func builtinCeil(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Ceil(n)), nil
}
func builtinAbs(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Abs(n)), nil
}
func builtinMin(i *Interpreter, args []Value) (Value, error) {
	if len(args) == 0 {
		return Value{}, fmt.Errorf("min() requires at least 1 argument")
	}
	m := math.Inf(1)
	for _, a := range args {
		if a.Kind != KindNumber {
			return Value{}, fmt.Errorf("min() requires numbers")
		}
		if a.Number < m {
			m = a.Number
		}
	}
	return NumberValue(m), nil
}
func builtinMax(i *Interpreter, args []Value) (Value, error) {
	if len(args) == 0 {
		return Value{}, fmt.Errorf("max() requires at least 1 argument")
	}
	m := math.Inf(-1)
	for _, a := range args {
		if a.Kind != KindNumber {
			return Value{}, fmt.Errorf("max() requires numbers")
		}
		if a.Number > m {
			m = a.Number
		}
	}
	return NumberValue(m), nil
}
func builtinRandom(i *Interpreter, args []Value) (Value, error) {
	if len(args) == 0 {
		return NumberValue(rand.Float64()), nil
	}
	if len(args) == 1 {
		n, err := numberArg(args, 0)
		if err != nil {
			return Value{}, err
		}
		return NumberValue(math.Floor(rand.Float64() * n)), nil
	}
	lo, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	hi, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(lo + math.Floor(rand.Float64()*(hi-lo))), nil
}

// ===== Type checks =====

func builtinIsString(i *Interpreter, args []Value) (Value, error) {
	return BoolValue(len(args) > 0 && args[0].Kind == KindString), nil
}
func builtinIsNumber(i *Interpreter, args []Value) (Value, error) {
	return BoolValue(len(args) > 0 && args[0].Kind == KindNumber), nil
}
func builtinIsBool(i *Interpreter, args []Value) (Value, error) {
	return BoolValue(len(args) > 0 && args[0].Kind == KindBool), nil
}
func builtinIsNull(i *Interpreter, args []Value) (Value, error) {
	return BoolValue(len(args) > 0 && args[0].Kind == KindNull), nil
}
func builtinIsArray(i *Interpreter, args []Value) (Value, error) {
	return BoolValue(len(args) > 0 && args[0].Kind == KindArray), nil
}
func builtinIsObject(i *Interpreter, args []Value) (Value, error) {
	return BoolValue(len(args) > 0 && args[0].Kind == KindObject), nil
}
func builtinIsFunction(i *Interpreter, args []Value) (Value, error) {
	return BoolValue(len(args) > 0 && args[0].Kind == KindFunction), nil
}

// ===== JSON helpers =====

func builtinJSONParse(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return jsonDecode([]byte(s))
}

func builtinJSONStringify(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return StringValue("null"), nil
	}
	b, err := jsonEncode(args[0])
	if err != nil {
		return Value{}, err
	}
	return StringValue(string(b)), nil
}

// ===== AI namespace =====

func builtinAIComplete(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("ai.complete(prompt, opts?) requires a prompt string")
	}
	prompt := args[0].String

	model := "gpt-4o-mini"
	apiKey := os.Getenv("OPENAI_API_KEY")
	maxTokens := 256

	if len(args) > 1 && args[1].Kind == KindObject {
		opts := args[1].Object
		if v, ok := opts.Get("model"); ok && v.Kind == KindString {
			model = v.String
		}
		if v, ok := opts.Get("max_tokens"); ok && v.Kind == KindNumber {
			maxTokens = int(v.Number)
		}
	}

	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.complete requires OPENAI_API_KEY environment variable")
	}

	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": maxTokens,
	})
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Value{}, err
	}
	if resp.StatusCode >= 400 {
		return Value{}, fmt.Errorf("ai.complete failed (%d): %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Value{}, err
	}
	if len(parsed.Choices) == 0 {
		return StringValue(""), nil
	}
	return StringValue(parsed.Choices[0].Message.Content), nil
}

func builtinAIEmbed(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("ai.embed(text) requires a text string")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.embed requires OPENAI_API_KEY environment variable")
	}
	body, _ := json.Marshal(map[string]any{
		"model": "text-embedding-3-small",
		"input": args[0].String,
	})
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Value{}, err
	}
	if resp.StatusCode >= 400 {
		return Value{}, fmt.Errorf("ai.embed failed (%d): %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Value{}, err
	}
	if len(parsed.Data) == 0 {
		return ArrayValue(nil), nil
	}
	out := make([]Value, len(parsed.Data[0].Embedding))
	for k, f := range parsed.Data[0].Embedding {
		out[k] = NumberValue(f)
	}
	return ArrayValue(out), nil
}
