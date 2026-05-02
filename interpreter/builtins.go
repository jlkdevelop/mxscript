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
	"net/smtp"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	def("print", builtinPrint)     // space-separated, with trailing newline
	def("println", builtinPrintln) // alias for print
	def("write", builtinWrite)     // space-separated, NO trailing newline
	def("eprint", builtinEprint)   // print to stderr (with newline)

	// --- HTTP response helpers ---
	def("json", builtinJSON)
	def("text", builtinText)
	def("html", builtinHTML)
	def("status", builtinStatus)
	def("redirect", builtinRedirect)

	// --- Environment / I/O ---
	def("env", builtinEnv)
	def("env_required", builtinEnvRequired)
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
	def("pad_left", builtinPadLeft)
	def("pad_right", builtinPadRight)
	def("repeat", builtinRepeat)
	def("substr", builtinSubstr)
	def("index_of", builtinIndexOf)
	def("html_escape", builtinHTMLEscape)
	def("html_unescape", builtinHTMLUnescape)
	def("slug", builtinSlug)

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
	def("sort", builtinSort)
	def("sort_by", builtinSortBy)
	def("reduce", builtinReduce)
	def("sum", builtinSum)
	def("group_by", builtinGroupBy)
	def("unique", builtinUnique)
	def("flatten", builtinFlatten)
	def("zip", builtinZip)

	// --- Math ---
	def("round", builtinRound)
	def("floor", builtinFloor)
	def("ceil", builtinCeil)
	def("abs", builtinAbs)
	def("min", builtinMin)
	def("max", builtinMax)
	def("random", builtinRandom)
	def("pow", builtinPow)
	def("sqrt", builtinSqrt)
	def("log", builtinLog)
	def("exp", builtinExp)

	// Math constants as a namespace object so we don't pollute globals.
	math := NewOrderedMap()
	math.Set("PI", NumberValue(3.141592653589793))
	math.Set("E", NumberValue(2.718281828459045))
	math.Set("INFINITY", NumberValue(math2Inf()))
	math.Set("NAN", NumberValue(math2NaN()))
	g.Set("math", ObjectValue(math))
	builtinNames["math"] = true

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

	// --- KV store (single-file JSON) ---
	def("kv_get", builtinKVGet)
	def("kv_set", builtinKVSet)
	def("kv_delete", builtinKVDelete)
	def("kv_keys", builtinKVKeys)
	def("kv_clear", builtinKVClear)

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
	def("add_days", builtinAddDays)
	def("add_hours", builtinAddHours)
	def("add_minutes", builtinAddMinutes)
	def("days_between", builtinDaysBetween)
	def("weekday", builtinWeekday)

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
	def("every", builtinEvery)
	def("after", builtinAfter)
	def("debounce", builtinDebounce)
	def("render", builtinRender)
	def("render_string", builtinRenderString)

	// --- Logger namespace ---
	logNS := NewOrderedMap()
	logNS.Set("info", FunctionValue(&Function{Name: "log.info", Native: builtinLogInfo}))
	logNS.Set("warn", FunctionValue(&Function{Name: "log.warn", Native: builtinLogWarn}))
	logNS.Set("error", FunctionValue(&Function{Name: "log.error", Native: builtinLogError}))
	logNS.Set("debug", FunctionValue(&Function{Name: "log.debug", Native: builtinLogDebug}))
	g.Set("log", ObjectValue(logNS))
	builtinNames["log"] = true

	// --- Email namespace ---
	emailNS := NewOrderedMap()
	emailNS.Set("send", FunctionValue(&Function{Name: "email.send", Native: builtinEmailSend}))
	g.Set("email", ObjectValue(emailNS))
	builtinNames["email"] = true

	// --- Webhook helpers ---
	def("verify_webhook", builtinVerifyWebhook)

	// --- Concurrency: channels ---
	def("chan", builtinChan)
	def("send", builtinChanSend)
	def("recv", builtinChanRecv)
	def("close_chan", builtinChanClose)
	def("wait_group", builtinWaitGroup)

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
	// print() historically added a trailing newline; we keep that behavior
	// because the bulk of MX programs in the wild rely on it. println() is
	// just a more explicit synonym.
	fmt.Println(strings.Join(parts, " "))
	return NullValue(), nil
}

func builtinPrintln(i *Interpreter, args []Value) (Value, error) {
	return builtinPrint(i, args)
}

func builtinWrite(i *Interpreter, args []Value) (Value, error) {
	parts := make([]string, len(args))
	for k, a := range args {
		parts[k] = a.Display()
	}
	fmt.Print(strings.Join(parts, " "))
	return NullValue(), nil
}

func builtinEprint(i *Interpreter, args []Value) (Value, error) {
	parts := make([]string, len(args))
	for k, a := range args {
		parts[k] = a.Display()
	}
	fmt.Fprintln(os.Stderr, strings.Join(parts, " "))
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

// env_required(name) returns the env var, or throws a descriptive error
// if it's unset / empty. Useful at startup to fail-fast on misconfiguration.
func builtinEnvRequired(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("env_required(name) requires a string name")
	}
	name := args[0].String
	v := os.Getenv(name)
	if v == "" {
		return Value{}, fmt.Errorf("required env var %q is not set", name)
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

// html_escape escapes the five HTML special chars: & < > " '. Use this
// before interpolating user input into html() responses to prevent XSS.
func builtinHTMLEscape(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return StringValue(r.Replace(s)), nil
}

func builtinHTMLUnescape(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
	)
	return StringValue(r.Replace(s)), nil
}

// slug turns "Hello, World!" into "hello-world" — useful for URL-safe IDs.
func builtinSlug(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	return StringValue(out), nil
}

// pad_left(s, width, ch?) pads s on the left with ch (default " ") until
// it has at least `width` runes. If s is already wide enough, returns s.
func builtinPadLeft(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	w, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	ch := " "
	if len(args) > 2 && args[2].Kind == KindString && args[2].String != "" {
		ch = string([]rune(args[2].String)[0:1])
	}
	missing := int(w) - len([]rune(s))
	if missing <= 0 {
		return StringValue(s), nil
	}
	return StringValue(strings.Repeat(ch, missing) + s), nil
}

func builtinPadRight(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	w, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	ch := " "
	if len(args) > 2 && args[2].Kind == KindString && args[2].String != "" {
		ch = string([]rune(args[2].String)[0:1])
	}
	missing := int(w) - len([]rune(s))
	if missing <= 0 {
		return StringValue(s), nil
	}
	return StringValue(s + strings.Repeat(ch, missing)), nil
}

func builtinRepeat(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	n, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		return Value{}, fmt.Errorf("repeat count must be non-negative")
	}
	return StringValue(strings.Repeat(s, int(n))), nil
}

// substr(s, start, length?) returns a slice. Negative start counts from the end.
func builtinSubstr(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	startF, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	runes := []rune(s)
	n := len(runes)
	start := int(startF)
	if start < 0 {
		start += n
	}
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	end := n
	if len(args) > 2 && args[2].Kind == KindNumber {
		length := int(args[2].Number)
		end = start + length
		if end > n {
			end = n
		}
		if end < start {
			end = start
		}
	}
	return StringValue(string(runes[start:end])), nil
}

// index_of(s, sub) returns the rune index of the first occurrence, or -1.
func builtinIndexOf(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	sub, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	byteIdx := strings.Index(s, sub)
	if byteIdx < 0 {
		return NumberValue(-1), nil
	}
	// Convert byte index to rune index for utf-8 correctness.
	return NumberValue(float64(len([]rune(s[:byteIdx])))), nil
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

// sort(arr) returns a sorted copy. Numbers ascending, strings
// lexicographic; mixed-kind arrays return an error.
func builtinSort(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("sort(arr) requires an array")
	}
	a := args[0].Array
	out := append([]Value(nil), a...)
	if len(out) < 2 {
		return ArrayValue(out), nil
	}
	kind := out[0].Kind
	for _, v := range out {
		if v.Kind != kind {
			return Value{}, fmt.Errorf("sort: cannot mix %s and %s", out[0].typeName(), v.typeName())
		}
	}
	switch kind {
	case KindNumber:
		// stdlib sort.Slice would be nicer, but we already imported sort.
		simpleSortFloat(out)
	case KindString:
		simpleSortString(out)
	default:
		return Value{}, fmt.Errorf("sort: unsupported element type %s", out[0].typeName())
	}
	return ArrayValue(out), nil
}

func simpleSortFloat(a []Value) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1].Number > a[j].Number; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func simpleSortString(a []Value) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1].String > a[j].String; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

// sort_by(arr, key_fn) sorts using a key extractor. Stable insertion sort.
func builtinSortBy(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("sort_by(arr, key_fn) requires (array, function)")
	}
	a := append([]Value(nil), args[0].Array...)
	keys := make([]Value, len(a))
	for k, v := range a {
		key, err := i.callFunction(nil, args[1].Function, []Value{v})
		if err != nil {
			return Value{}, err
		}
		keys[k] = key
	}
	for k := 1; k < len(a); k++ {
		for j := k; j > 0 && less(keys[j-1], keys[j]) == false && less(keys[j], keys[j-1]); j-- {
			a[j-1], a[j] = a[j], a[j-1]
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return ArrayValue(a), nil
}

func less(a, b Value) bool {
	if a.Kind == KindNumber && b.Kind == KindNumber {
		return a.Number < b.Number
	}
	if a.Kind == KindString && b.Kind == KindString {
		return a.String < b.String
	}
	return false
}

// reduce(arr, fn, init) folds an array into a single value.
func builtinReduce(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 3 || args[0].Kind != KindArray || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("reduce(arr, fn, init) requires (array, function, value)")
	}
	acc := args[2]
	for _, el := range args[0].Array {
		v, err := i.callFunction(nil, args[1].Function, []Value{acc, el})
		if err != nil {
			return Value{}, err
		}
		acc = v
	}
	return acc, nil
}

// sum(arr) — sum of numeric array elements.
func builtinSum(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("sum(arr) requires an array")
	}
	total := 0.0
	for _, v := range args[0].Array {
		if v.Kind != KindNumber {
			return Value{}, fmt.Errorf("sum: non-numeric element %s", v.typeName())
		}
		total += v.Number
	}
	return NumberValue(total), nil
}

// group_by(arr, key_fn) -> object mapping key -> array of items.
func builtinGroupBy(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("group_by(arr, key_fn) requires (array, function)")
	}
	out := NewOrderedMap()
	for _, v := range args[0].Array {
		key, err := i.callFunction(nil, args[1].Function, []Value{v})
		if err != nil {
			return Value{}, err
		}
		k := key.Display()
		bucket, _ := out.Get(k)
		if bucket.Kind != KindArray {
			bucket = ArrayValue(nil)
		}
		bucket.Array = append(bucket.Array, v)
		out.Set(k, bucket)
	}
	return ObjectValue(out), nil
}

// unique(arr) returns a new array with duplicates removed (first wins).
func builtinUnique(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("unique(arr) requires an array")
	}
	var out []Value
	for _, v := range args[0].Array {
		dup := false
		for _, w := range out {
			if valuesEqual(v, w) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, v)
		}
	}
	return ArrayValue(out), nil
}

// flatten(arr) flattens one level of nested arrays.
func builtinFlatten(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("flatten(arr) requires an array")
	}
	var out []Value
	for _, v := range args[0].Array {
		if v.Kind == KindArray {
			out = append(out, v.Array...)
		} else {
			out = append(out, v)
		}
	}
	return ArrayValue(out), nil
}

// zip(a, b) -> array of [a[i], b[i]] pairs, length = min(len(a), len(b)).
func builtinZip(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray || args[1].Kind != KindArray {
		return Value{}, fmt.Errorf("zip(a, b) requires two arrays")
	}
	a, b := args[0].Array, args[1].Array
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]Value, n)
	for k := 0; k < n; k++ {
		out[k] = ArrayValue([]Value{a[k], b[k]})
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
func builtinPow(i *Interpreter, args []Value) (Value, error) {
	base, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	exp, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Pow(base, exp)), nil
}
func builtinSqrt(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Sqrt(n)), nil
}
func builtinLog(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Log(n)), nil
}
func builtinExp(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return NumberValue(math.Exp(n)), nil
}
func math2Inf() float64 { return math.Inf(1) }
func math2NaN() float64 { return math.NaN() }

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

// ===== KV store =====
//
// A tiny JSON-file-backed key/value store. Each operation reads, mutates,
// and writes the file atomically (write to a tmp file, then rename). Good
// enough for prototypes / hobby apps; not a replacement for SQLite at
// scale.

var kvLock sync.Mutex

func loadKV(path string) (*OrderedMap, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return NewOrderedMap(), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return NewOrderedMap(), nil
	}
	v, err := jsonDecode(raw)
	if err != nil {
		return nil, err
	}
	if v.Kind != KindObject {
		return nil, fmt.Errorf("kv file %s does not contain a JSON object", path)
	}
	return v.Object, nil
}

func saveKV(path string, om *OrderedMap) error {
	b, err := jsonEncode(ObjectValue(om))
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func builtinKVGet(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	kvLock.Lock()
	defer kvLock.Unlock()
	om, err := loadKV(path)
	if err != nil {
		return Value{}, err
	}
	v, ok := om.Get(key)
	if !ok {
		return NullValue(), nil
	}
	return v, nil
}

func builtinKVSet(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 3 {
		return Value{}, fmt.Errorf("kv_set(path, key, value) requires 3 arguments")
	}
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	kvLock.Lock()
	defer kvLock.Unlock()
	om, err := loadKV(path)
	if err != nil {
		return Value{}, err
	}
	om.Set(key, args[2])
	if err := saveKV(path, om); err != nil {
		return Value{}, err
	}
	return args[2], nil
}

func builtinKVDelete(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	kvLock.Lock()
	defer kvLock.Unlock()
	om, err := loadKV(path)
	if err != nil {
		return Value{}, err
	}
	if _, ok := om.Get(key); !ok {
		return BoolValue(false), nil
	}
	delete(om.Values, key)
	for k, v := range om.Keys {
		if v == key {
			om.Keys = append(om.Keys[:k], om.Keys[k+1:]...)
			break
		}
	}
	if err := saveKV(path, om); err != nil {
		return Value{}, err
	}
	return BoolValue(true), nil
}

func builtinKVKeys(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	kvLock.Lock()
	defer kvLock.Unlock()
	om, err := loadKV(path)
	if err != nil {
		return Value{}, err
	}
	out := make([]Value, len(om.Keys))
	for k, key := range om.Keys {
		out[k] = StringValue(key)
	}
	return ArrayValue(out), nil
}

func builtinKVClear(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	kvLock.Lock()
	defer kvLock.Unlock()
	return NullValue(), saveKV(path, NewOrderedMap())
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

// render(path, vars?) reads a template file from disk and substitutes
// any `${expr}` placeholders. Variables come from `vars` (an object) and
// support dotted access for nested values. Reasonably robust against
// the most common XSS pitfalls because all substituted values are
// auto html-escaped — call render_string for raw passthrough.
func builtinRender(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return Value{}, err
	}
	var vars *OrderedMap
	if len(args) > 1 && args[1].Kind == KindObject {
		vars = args[1].Object
	}
	out, err := renderTemplate(string(src), vars, true)
	if err != nil {
		return Value{}, err
	}
	return ResponseValue(&Response{ContentType: "text/html; charset=utf-8", Body: StringValue(out)}), nil
}

// render_string(template, vars?) is the same as render() but takes the
// template inline. Returns a plain string — caller decides what to do
// with it (html(), text(), persistence, etc.).
func builtinRenderString(i *Interpreter, args []Value) (Value, error) {
	tmpl, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	var vars *OrderedMap
	if len(args) > 1 && args[1].Kind == KindObject {
		vars = args[1].Object
	}
	out, err := renderTemplate(tmpl, vars, true)
	if err != nil {
		return Value{}, err
	}
	return StringValue(out), nil
}

// renderTemplate replaces `{{ path.expr }}` placeholders with the looked-up
// value (HTML-escaped if `escape` is true). `{{{ path }}}` is the raw form
// — no escaping. Templates use `{{` instead of `${` so they don't collide
// with MX's own string interpolation in the surrounding source.
func renderTemplate(tmpl string, vars *OrderedMap, escape bool) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(tmpl) {
		// Triple-brace raw form first.
		if i+2 < len(tmpl) && tmpl[i] == '{' && tmpl[i+1] == '{' && tmpl[i+2] == '{' {
			end := strings.Index(tmpl[i+3:], "}}}")
			if end < 0 {
				return "", fmt.Errorf("unterminated {{{...}}} in template")
			}
			expr := strings.TrimSpace(tmpl[i+3 : i+3+end])
			val := lookupTemplateVar(vars, expr)
			b.WriteString(val.Display())
			i += 3 + end + 3
			continue
		}
		// Standard {{ }} form (HTML-escaped).
		if i+1 < len(tmpl) && tmpl[i] == '{' && tmpl[i+1] == '{' {
			end := strings.Index(tmpl[i+2:], "}}")
			if end < 0 {
				return "", fmt.Errorf("unterminated {{...}} in template")
			}
			expr := strings.TrimSpace(tmpl[i+2 : i+2+end])
			val := lookupTemplateVar(vars, expr)
			s := val.Display()
			if escape {
				s = htmlEscapeString(s)
			}
			b.WriteString(s)
			i += 2 + end + 2
			continue
		}
		b.WriteByte(tmpl[i])
		i++
	}
	return b.String(), nil
}

func lookupTemplateVar(vars *OrderedMap, expr string) Value {
	if vars == nil || expr == "" {
		return NullValue()
	}
	parts := strings.Split(expr, ".")
	v, ok := vars.Get(parts[0])
	if !ok {
		return NullValue()
	}
	for _, p := range parts[1:] {
		if v.Kind != KindObject {
			return NullValue()
		}
		var found bool
		v, found = v.Object.Get(p)
		if !found {
			return NullValue()
		}
	}
	return v
}

func htmlEscapeString(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// ===== Concurrency: channels =====
//
// chan(capacity?)  -> channel
// send(ch, value)  -> null  (blocks if buffered chan is full)
// recv(ch)         -> value | null  (null means closed)
// close_chan(ch)   -> null
// wait_group()     -> { add, done, wait }  — sync.WaitGroup wrapper

func builtinChan(i *Interpreter, args []Value) (Value, error) {
	cap := 0
	if len(args) > 0 && args[0].Kind == KindNumber {
		cap = int(args[0].Number)
	}
	return ChannelValue(&Channel{C: make(chan Value, cap)}), nil
}

func builtinChanSend(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindChannel {
		return Value{}, fmt.Errorf("send(ch, value) requires a channel and a value")
	}
	defer func() {
		// Sending on a closed channel panics; convert to error.
		_ = recover()
	}()
	args[0].Channel.C <- args[1]
	return NullValue(), nil
}

func builtinChanRecv(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindChannel {
		return Value{}, fmt.Errorf("recv(ch) requires a channel")
	}
	v, ok := <-args[0].Channel.C
	if !ok {
		return NullValue(), nil
	}
	return v, nil
}

func builtinChanClose(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindChannel {
		return Value{}, fmt.Errorf("close_chan(ch) requires a channel")
	}
	args[0].Channel.Close()
	return NullValue(), nil
}

// wait_group() returns an object with add(n), done(), wait() methods
// wrapping a sync.WaitGroup. Useful for "fork N goroutines, wait for all".
func builtinWaitGroup(i *Interpreter, args []Value) (Value, error) {
	wg := &sync.WaitGroup{}
	out := NewOrderedMap()
	out.Set("add", FunctionValue(&Function{Name: "wg.add", Native: func(_ *Interpreter, a []Value) (Value, error) {
		n := 1
		if len(a) > 0 && a[0].Kind == KindNumber {
			n = int(a[0].Number)
		}
		wg.Add(n)
		return NullValue(), nil
	}}))
	out.Set("done", FunctionValue(&Function{Name: "wg.done", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		wg.Done()
		return NullValue(), nil
	}}))
	out.Set("wait", FunctionValue(&Function{Name: "wg.wait", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		wg.Wait()
		return NullValue(), nil
	}}))
	return ObjectValue(out), nil
}

// ===== Email (SMTP) =====
//
// email.send(opts) sends a plaintext (or HTML) email through an SMTP relay.
// Required keys: host, from, to, subject, body. Optional: port (default
// 587), username, password, use_tls (default true), html (bool).
//
//	email.send({
//	  host: env_required("SMTP_HOST"),
//	  port: 587,
//	  username: env("SMTP_USER"),
//	  password: env("SMTP_PASS"),
//	  from: "noreply@mxscript.com",
//	  to: "user@example.com",
//	  subject: "Welcome",
//	  body: "Thanks for signing up!"
//	})

func builtinEmailSend(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("email.send(opts) requires an object")
	}
	o := args[0].Object
	getStr := func(k string) string {
		if v, ok := o.Get(k); ok && v.Kind == KindString {
			return v.String
		}
		return ""
	}
	getInt := func(k string, dflt int) int {
		if v, ok := o.Get(k); ok && v.Kind == KindNumber {
			return int(v.Number)
		}
		return dflt
	}
	getBool := func(k string, dflt bool) bool {
		if v, ok := o.Get(k); ok && v.Kind == KindBool {
			return v.Bool
		}
		return dflt
	}
	host := getStr("host")
	port := getInt("port", 587)
	from := getStr("from")
	to := getStr("to")
	subject := getStr("subject")
	body := getStr("body")
	username := getStr("username")
	password := getStr("password")
	isHTML := getBool("html", false)

	if host == "" || from == "" || to == "" {
		return Value{}, fmt.Errorf("email.send requires host, from, to")
	}

	contentType := "text/plain; charset=\"UTF-8\""
	if isHTML {
		contentType = "text/html; charset=\"UTF-8\""
	}
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n%s",
		from, to, subject, contentType, body))

	addr := fmt.Sprintf("%s:%d", host, port)
	var auth smtp.Auth
	if username != "" && password != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	if err := smtp.SendMail(addr, auth, from, []string{to}, msg); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

// verify_webhook(secret, body, signature, [scheme]) returns true if the
// signature matches an HMAC-SHA256 of the body. `scheme` controls the
// signature format:
//
//	"hex"          — raw hex digest (default)
//	"base64"       — base64-encoded digest
//	"github"       — "sha256=<hex>" (GitHub style)
//	"stripe"       — "t=<ts>,v1=<hex>" (signature must include the timestamp)
func builtinVerifyWebhook(i *Interpreter, args []Value) (Value, error) {
	secret, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	body, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	sig, err := stringArg(args, 2)
	if err != nil {
		return Value{}, err
	}
	scheme := "hex"
	if len(args) > 3 && args[3].Kind == KindString {
		scheme = args[3].String
	}

	switch scheme {
	case "github":
		// "sha256=<hex>"
		const prefix = "sha256="
		if !strings.HasPrefix(sig, prefix) {
			return BoolValue(false), nil
		}
		expected := computeHMACHex(secret, body)
		return BoolValue(hmac.Equal([]byte(expected), []byte(sig[len(prefix):]))), nil
	case "stripe":
		// "t=<ts>,v1=<hex>" — Stripe signs "<timestamp>.<body>"
		var ts, hex string
		for _, part := range strings.Split(sig, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "t":
				ts = kv[1]
			case "v1":
				hex = kv[1]
			}
		}
		if ts == "" || hex == "" {
			return BoolValue(false), nil
		}
		expected := computeHMACHex(secret, ts+"."+body)
		return BoolValue(hmac.Equal([]byte(expected), []byte(hex))), nil
	case "base64":
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(body))
		expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		return BoolValue(hmac.Equal([]byte(expected), []byte(sig))), nil
	default: // hex
		expected := computeHMACHex(secret, body)
		return BoolValue(hmac.Equal([]byte(expected), []byte(sig))), nil
	}
}

func computeHMACHex(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// ===== Logger =====

func logEmit(level, color string, args []Value) {
	parts := make([]string, len(args))
	for k, a := range args {
		parts[k] = a.Display()
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(os.Stderr, "%s%s%s %s%s%s %s\n",
		"\033[0;90m", stamp, "\033[0m",
		color, level, "\033[0m",
		strings.Join(parts, " "))
}

func builtinLogInfo(i *Interpreter, args []Value) (Value, error) {
	logEmit("INFO ", "\033[1;36m", args)
	return NullValue(), nil
}
func builtinLogWarn(i *Interpreter, args []Value) (Value, error) {
	logEmit("WARN ", "\033[1;33m", args)
	return NullValue(), nil
}
func builtinLogError(i *Interpreter, args []Value) (Value, error) {
	logEmit("ERROR", "\033[1;31m", args)
	return NullValue(), nil
}
func builtinLogDebug(i *Interpreter, args []Value) (Value, error) {
	logEmit("DEBUG", "\033[0;90m", args)
	return NullValue(), nil
}

// ===== Date arithmetic =====

func msToTime(ms float64) time.Time { return time.UnixMilli(int64(ms)).UTC() }

func builtinAddDays(i *Interpreter, args []Value) (Value, error) {
	ms, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	n, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	t := msToTime(ms).AddDate(0, 0, int(n))
	return NumberValue(float64(t.UnixMilli())), nil
}
func builtinAddHours(i *Interpreter, args []Value) (Value, error) {
	ms, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	n, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	t := msToTime(ms).Add(time.Duration(n) * time.Hour)
	return NumberValue(float64(t.UnixMilli())), nil
}
func builtinAddMinutes(i *Interpreter, args []Value) (Value, error) {
	ms, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	n, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	t := msToTime(ms).Add(time.Duration(n) * time.Minute)
	return NumberValue(float64(t.UnixMilli())), nil
}
func builtinDaysBetween(i *Interpreter, args []Value) (Value, error) {
	a, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	b, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	d := msToTime(b).Sub(msToTime(a))
	return NumberValue(d.Hours() / 24), nil
}
func builtinWeekday(i *Interpreter, args []Value) (Value, error) {
	ms, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(msToTime(ms).Weekday().String()), nil
}

// every(duration, fn) runs fn() in a goroutine every `duration` (number=ms
// or string like "5s"). Returns a stop function — call it to cancel.
//
//	let stop = every("5s", fn() { print("tick", now_iso()) })
//	// later... stop()
func builtinEvery(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("every(duration, fn) requires (duration, function)")
	}
	d, err := durationFromValue(args[0])
	if err != nil {
		return Value{}, err
	}
	fn := args[1].Function
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(d)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := i.callFunction(nil, fn, nil); err != nil {
					fmt.Fprintf(os.Stderr, "[mx every] %v\n", err)
					return
				}
			}
		}
	}()
	cancel := &Function{Name: "stop", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		select {
		case <-stop: // already closed
		default:
			close(stop)
		}
		return NullValue(), nil
	}}
	return FunctionValue(cancel), nil
}

// after(duration, fn) runs fn() once after `duration`. Returns a cancel fn.
func builtinAfter(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("after(duration, fn) requires (duration, function)")
	}
	d, err := durationFromValue(args[0])
	if err != nil {
		return Value{}, err
	}
	fn := args[1].Function
	stop := make(chan struct{})
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(d):
			if _, err := i.callFunction(nil, fn, nil); err != nil {
				fmt.Fprintf(os.Stderr, "[mx after] %v\n", err)
			}
		}
	}()
	cancel := &Function{Name: "cancel", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		select {
		case <-stop:
		default:
			close(stop)
		}
		return NullValue(), nil
	}}
	return FunctionValue(cancel), nil
}

// debounce(duration, fn) returns a wrapper that, when called repeatedly,
// only fires fn() once `duration` has passed since the last call.
func builtinDebounce(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("debounce(duration, fn) requires (duration, function)")
	}
	d, err := durationFromValue(args[0])
	if err != nil {
		return Value{}, err
	}
	fn := args[1].Function
	var mu sync.Mutex
	var timer *time.Timer
	wrapper := &Function{Name: "debounced", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(d, func() {
			if _, err := i.callFunction(nil, fn, nil); err != nil {
				fmt.Fprintf(os.Stderr, "[mx debounce] %v\n", err)
			}
		})
		return NullValue(), nil
	}}
	return FunctionValue(wrapper), nil
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
