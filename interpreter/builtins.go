// builtins.go installs the MX Script standard library into the global
// environment. Every native function is registered here so they're
// available in every .mx program without an import.
package interpreter

import (
	"bytes"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// builtinNames lists every name registered into the global environment by
// registerBuiltins. The REPL uses it to filter built-ins out of `.vars`.
var builtinNames = map[string]bool{}

// IsBuiltin reports whether a global name was installed by the standard
// library rather than the user's program.
func IsBuiltin(name string) bool { return builtinNames[name] }

func registerBuiltins(i *Interpreter) {
	g := i.globals

	def := func(name string, fn func(interp *Interpreter, args []Value) (Value, error)) {
		g.Set(name, FunctionValue(&Function{Name: name, Native: fn}))
		builtinNames[name] = true
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

	// --- File I/O ---
	def("read_file", builtinReadFile)
	def("write_file", builtinWriteFile)
	def("file_exists", builtinFileExists)
	def("list_files", builtinListFiles)
	def("delete_file", builtinDeleteFile)

	// --- Crypto / encoding ---
	def("hash_sha256", builtinHashSHA256)
	def("hmac_sha256", builtinHmacSHA256)
	def("base64_encode", builtinBase64Encode)
	def("base64_decode", builtinBase64Decode)
	def("uuid", builtinUUID)

	// --- Regex ---
	def("re_match", builtinReMatch)
	def("re_find", builtinReFind)
	def("re_find_all", builtinReFindAll)
	def("re_replace", builtinReReplace)

	// --- Time helpers ---
	def("now_iso", builtinNowISO)
	def("parse_date", builtinParseDate)
	def("format_date", builtinFormatDate)

	// --- URL helpers ---
	def("parse_url", builtinParseURL)
	def("url_encode", builtinURLEncode)
	def("url_decode", builtinURLDecode)

	// --- Misc ---
	def("retry", builtinRetry)
	def("assert", builtinAssert)
	def("assert_eq", builtinAssertEq)
	def("sign_cookie", builtinSignCookie)
	def("verify_cookie", builtinVerifyCookie)

	// --- AI namespace ---
	ai := NewOrderedMap()
	ai.Set("complete", FunctionValue(&Function{Name: "ai.complete", Native: builtinAIComplete}))
	ai.Set("embed", FunctionValue(&Function{Name: "ai.embed", Native: builtinAIEmbed}))
	g.Set("ai", ObjectValue(ai))
	builtinNames["ai"] = true

	// --- JWT namespace ---
	jwt := NewOrderedMap()
	jwt.Set("sign", FunctionValue(&Function{Name: "jwt.sign", Native: builtinJWTSign}))
	jwt.Set("verify", FunctionValue(&Function{Name: "jwt.verify", Native: builtinJWTVerify}))
	g.Set("jwt", ObjectValue(jwt))
	builtinNames["jwt"] = true
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
	resp := &Response{ContentType: "application/json", Body: body}
	if len(args) > 1 {
		if err := applyResponseOpts(resp, args[1]); err != nil {
			return Value{}, err
		}
	}
	return ResponseValue(resp), nil
}

func builtinText(i *Interpreter, args []Value) (Value, error) {
	var body Value = StringValue("")
	if len(args) > 0 {
		body = args[0]
	}
	if body.Kind != KindString {
		body = StringValue(body.Display())
	}
	resp := &Response{ContentType: "text/plain; charset=utf-8", Body: body}
	if len(args) > 1 {
		if err := applyResponseOpts(resp, args[1]); err != nil {
			return Value{}, err
		}
	}
	return ResponseValue(resp), nil
}

func builtinHTML(i *Interpreter, args []Value) (Value, error) {
	var body Value = StringValue("")
	if len(args) > 0 {
		body = args[0]
	}
	if body.Kind != KindString {
		body = StringValue(body.Display())
	}
	resp := &Response{ContentType: "text/html; charset=utf-8", Body: body}
	if len(args) > 1 {
		if err := applyResponseOpts(resp, args[1]); err != nil {
			return Value{}, err
		}
	}
	return ResponseValue(resp), nil
}

func builtinStatus(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return Value{}, fmt.Errorf("status(code, body?, opts?) requires a numeric status code")
	}
	resp := &Response{Status: int(args[0].Number), ContentType: "application/json"}
	if len(args) > 1 {
		resp.Body = args[1]
	}
	if len(args) > 2 {
		if err := applyResponseOpts(resp, args[2]); err != nil {
			return Value{}, err
		}
	}
	return ResponseValue(resp), nil
}

// applyResponseOpts merges an options object into the response. Recognised
// keys: `cookies` (array of cookie objects), `headers` (object of strings).
func applyResponseOpts(resp *Response, optsVal Value) error {
	if optsVal.Kind != KindObject {
		return fmt.Errorf("response options must be an object")
	}
	opts := optsVal.Object
	if v, ok := opts.Get("headers"); ok {
		if v.Kind != KindObject {
			return fmt.Errorf("opts.headers must be an object")
		}
		if resp.Headers == nil {
			resp.Headers = map[string]string{}
		}
		for _, k := range v.Object.Keys {
			hv, _ := v.Object.Get(k)
			if hv.Kind != KindString {
				return fmt.Errorf("header %q must be a string", k)
			}
			resp.Headers[k] = hv.String
		}
	}
	if v, ok := opts.Get("cookies"); ok {
		if v.Kind != KindArray {
			return fmt.Errorf("opts.cookies must be an array")
		}
		for _, c := range v.Array {
			cookie, err := mxCookieToHTTP(c)
			if err != nil {
				return err
			}
			resp.Cookies = append(resp.Cookies, cookie)
		}
	}
	return nil
}

// mxCookieToHTTP converts an MX cookie object into a net/http Cookie.
func mxCookieToHTTP(v Value) (*http.Cookie, error) {
	if v.Kind != KindObject {
		return nil, fmt.Errorf("cookie must be an object with at least name and value")
	}
	getStr := func(k string) string {
		if val, ok := v.Object.Get(k); ok && val.Kind == KindString {
			return val.String
		}
		return ""
	}
	getNum := func(k string) int {
		if val, ok := v.Object.Get(k); ok && val.Kind == KindNumber {
			return int(val.Number)
		}
		return 0
	}
	getBool := func(k string) bool {
		if val, ok := v.Object.Get(k); ok && val.Kind == KindBool {
			return val.Bool
		}
		return false
	}
	c := &http.Cookie{
		Name:     getStr("name"),
		Value:    getStr("value"),
		Path:     getStr("path"),
		Domain:   getStr("domain"),
		MaxAge:   getNum("max_age"),
		HttpOnly: getBool("http_only"),
		Secure:   getBool("secure"),
	}
	if c.Name == "" {
		return nil, fmt.Errorf("cookie.name is required")
	}
	switch strings.ToLower(getStr("same_site")) {
	case "strict":
		c.SameSite = http.SameSiteStrictMode
	case "lax":
		c.SameSite = http.SameSiteLaxMode
	case "none":
		c.SameSite = http.SameSiteNoneMode
	}
	return c, nil
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
	pretty := false
	if len(args) > 1 && args[1].IsTruthy() {
		pretty = true
	}
	if !pretty {
		return StringValue(string(b)), nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return Value{}, err
	}
	return StringValue(buf.String()), nil
}

// ===== File I/O =====

func builtinReadFile(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Value{}, err
	}
	return StringValue(string(data)), nil
}

func builtinWriteFile(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	content, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

func builtinFileExists(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	_, err = os.Stat(path)
	return BoolValue(err == nil), nil
}

func builtinListFiles(i *Interpreter, args []Value) (Value, error) {
	dir, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Value{}, err
	}
	out := make([]Value, 0, len(entries))
	for _, e := range entries {
		out = append(out, StringValue(filepath.Join(dir, e.Name())))
	}
	return ArrayValue(out), nil
}

func builtinDeleteFile(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	if err := os.Remove(path); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

// ===== Crypto / encoding =====

func builtinHashSHA256(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	h := sha256.Sum256([]byte(s))
	return StringValue(hex.EncodeToString(h[:])), nil
}

func builtinBase64Encode(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(base64.StdEncoding.EncodeToString([]byte(s))), nil
}

func builtinBase64Decode(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	out, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return Value{}, err
	}
	return StringValue(string(out)), nil
}

// builtinUUID returns an RFC 4122 v4 UUID using crypto/rand.
func builtinUUID(i *Interpreter, args []Value) (Value, error) {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		return Value{}, err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return StringValue(fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])), nil
}

// ===== Time =====

func builtinNowISO(i *Interpreter, args []Value) (Value, error) {
	return StringValue(time.Now().UTC().Format(time.RFC3339Nano)), nil
}

// parse_date(s, layout?) -> unix milliseconds or null. The layout defaults
// to RFC 3339; the user can pass any Go time-format reference layout.
func builtinParseDate(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	layout := time.RFC3339
	if len(args) > 1 && args[1].Kind == KindString {
		layout = args[1].String
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return NullValue(), nil
	}
	return NumberValue(float64(t.UnixMilli())), nil
}

// format_date(unix_ms, layout?) -> string. Default layout is RFC 3339 UTC.
func builtinFormatDate(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindNumber {
		return Value{}, fmt.Errorf("format_date(unix_ms, layout?) requires a number")
	}
	t := time.UnixMilli(int64(args[0].Number)).UTC()
	layout := time.RFC3339
	if len(args) > 1 && args[1].Kind == KindString {
		layout = args[1].String
	}
	return StringValue(t.Format(layout)), nil
}

func builtinHmacSHA256(i *Interpreter, args []Value) (Value, error) {
	secret, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	msg, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return StringValue(hex.EncodeToString(mac.Sum(nil))), nil
}

// ===== Regex =====

func compileRegex(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}

// re_match(pattern, s) -> bool
func builtinReMatch(i *Interpreter, args []Value) (Value, error) {
	pattern, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	s, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	re, err := compileRegex(pattern)
	if err != nil {
		return Value{}, err
	}
	return BoolValue(re.MatchString(s)), nil
}

// re_find(pattern, s) -> first match string, or null
func builtinReFind(i *Interpreter, args []Value) (Value, error) {
	pattern, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	s, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	re, err := compileRegex(pattern)
	if err != nil {
		return Value{}, err
	}
	if m := re.FindStringSubmatch(s); m != nil {
		// If there are capture groups, return them as an array.
		if len(m) > 1 {
			out := make([]Value, len(m))
			for k, g := range m {
				out[k] = StringValue(g)
			}
			return ArrayValue(out), nil
		}
		return StringValue(m[0]), nil
	}
	return NullValue(), nil
}

// re_find_all(pattern, s) -> array of all matches
func builtinReFindAll(i *Interpreter, args []Value) (Value, error) {
	pattern, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	s, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	re, err := compileRegex(pattern)
	if err != nil {
		return Value{}, err
	}
	matches := re.FindAllString(s, -1)
	out := make([]Value, len(matches))
	for k, m := range matches {
		out[k] = StringValue(m)
	}
	return ArrayValue(out), nil
}

// re_replace(pattern, s, replacement) -> string
func builtinReReplace(i *Interpreter, args []Value) (Value, error) {
	pattern, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	s, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	repl, err := stringArg(args, 2)
	if err != nil {
		return Value{}, err
	}
	re, err := compileRegex(pattern)
	if err != nil {
		return Value{}, err
	}
	return StringValue(re.ReplaceAllString(s, repl)), nil
}

// ===== JWT (HS256) =====

func builtinJWTSign(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("jwt.sign(payload, secret) requires 2 arguments")
	}
	if args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("jwt.sign payload must be an object")
	}
	if args[1].Kind != KindString {
		return Value{}, fmt.Errorf("jwt.sign secret must be a string")
	}

	header := []byte(`{"alg":"HS256","typ":"JWT"}`)
	payloadBytes, err := jsonEncode(args[0])
	if err != nil {
		return Value{}, err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(header) + "." + enc.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, []byte(args[1].String))
	mac.Write([]byte(signingInput))
	sig := enc.EncodeToString(mac.Sum(nil))
	return StringValue(signingInput + "." + sig), nil
}

func builtinJWTVerify(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("jwt.verify(token, secret) requires 2 arguments")
	}
	if args[0].Kind != KindString || args[1].Kind != KindString {
		return Value{}, fmt.Errorf("jwt.verify expects (string, string)")
	}
	token := args[0].String
	secret := args[1].String

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return NullValue(), nil
	}
	enc := base64.RawURLEncoding

	// Verify signature.
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)
	got, err := enc.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, got) {
		return NullValue(), nil
	}

	// Decode payload.
	payloadBytes, err := enc.DecodeString(parts[1])
	if err != nil {
		return NullValue(), nil
	}
	v, err := jsonDecode(payloadBytes)
	if err != nil {
		return NullValue(), nil
	}

	// Honor the `exp` claim if present (Unix seconds).
	if v.Kind == KindObject {
		if exp, ok := v.Object.Get("exp"); ok && exp.Kind == KindNumber {
			if int64(exp.Number) < time.Now().Unix() {
				return NullValue(), nil
			}
		}
	}
	return v, nil
}

// ===== URL =====

// parse_url(s) -> object with scheme, host, port, path, query (object), fragment, raw
func builtinParseURL(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	u, err := neturl.Parse(s)
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	out.Set("scheme", StringValue(u.Scheme))
	out.Set("host", StringValue(u.Hostname()))
	out.Set("port", StringValue(u.Port()))
	out.Set("path", StringValue(u.Path))
	q := NewOrderedMap()
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			q.Set(k, StringValue(vs[0]))
		}
	}
	out.Set("query", ObjectValue(q))
	out.Set("fragment", StringValue(u.Fragment))
	out.Set("raw", StringValue(s))
	return ObjectValue(out), nil
}

func builtinURLEncode(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(neturl.QueryEscape(s)), nil
}

func builtinURLDecode(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	out, err := neturl.QueryUnescape(s)
	if err != nil {
		return Value{}, err
	}
	return StringValue(out), nil
}

// ===== Misc =====

// assert(cond, msg?) throws if cond is falsy. Used by `mx test`.
func builtinAssert(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return Value{}, fmt.Errorf("assert(cond, msg?) requires at least 1 argument")
	}
	if !args[0].IsTruthy() {
		msg := "assertion failed"
		if len(args) > 1 {
			msg = "assertion failed: " + args[1].Display()
		}
		return Value{}, fmt.Errorf("%s", msg)
	}
	return NullValue(), nil
}

// assert_eq(a, b, msg?) throws if a != b. Includes both values in the
// error message for easier debugging.
func builtinAssertEq(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("assert_eq(a, b, msg?) requires at least 2 arguments")
	}
	if !valuesEqual(args[0], args[1]) {
		prefix := "assert_eq failed"
		if len(args) > 2 {
			prefix = "assert_eq failed: " + args[2].Display()
		}
		return Value{}, fmt.Errorf("%s — left: %s, right: %s", prefix, args[0].Display(), args[1].Display())
	}
	return NullValue(), nil
}

// sign_cookie(secret, value) returns "value.signature" — a tamper-evident
// signed string suitable for session cookies. Cheaper than a JWT when you
// just need integrity.
func builtinSignCookie(i *Interpreter, args []Value) (Value, error) {
	secret, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	value, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return StringValue(value + "." + sig), nil
}

// verify_cookie(secret, signed) returns the original value if the signature
// is intact, or null if the cookie has been tampered with.
func builtinVerifyCookie(i *Interpreter, args []Value) (Value, error) {
	secret, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	signed, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	idx := strings.LastIndex(signed, ".")
	if idx < 0 {
		return NullValue(), nil
	}
	value := signed[:idx]
	gotSig, err := base64.RawURLEncoding.DecodeString(signed[idx+1:])
	if err != nil {
		return NullValue(), nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	if !hmac.Equal(mac.Sum(nil), gotSig) {
		return NullValue(), nil
	}
	return StringValue(value), nil
}

// retry(fn, attempts, delay_ms?) — call fn() up to `attempts` times,
// returning the first non-error result. delay_ms defaults to 100.
func builtinRetry(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindFunction || args[1].Kind != KindNumber {
		return Value{}, fmt.Errorf("retry(fn, attempts, delay_ms?) requires (function, number, [number])")
	}
	attempts := int(args[1].Number)
	delay := 100
	if len(args) > 2 && args[2].Kind == KindNumber {
		delay = int(args[2].Number)
	}
	var lastErr error
	for k := 0; k < attempts; k++ {
		v, err := i.callFunction(nil, args[0].Function, nil)
		if err == nil {
			return v, nil
		}
		lastErr = err
		if k < attempts-1 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
	return Value{}, lastErr
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
