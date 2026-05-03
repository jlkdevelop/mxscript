// builtins.go installs the MX Script standard library into the global
// environment. Every native function is registered here so they're
// available in every .mx program without an import.
package interpreter

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif" // register decoders
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/smtp"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// builtinNames lists every name registered into the global environment by
// registerBuiltins. The REPL uses it to filter built-ins out of `.vars`.
var builtinNames = map[string]bool{}

// serverStartTime is captured the first time builtins are registered;
// status_page() uses it to compute uptime.
var serverStartTime = time.Now()

// IsBuiltin reports whether a global name was installed by the standard
// library rather than the user's program. The map is populated by
// registerBuiltins, which only runs when an Interpreter is constructed.
// Static-analysis callers (the checker) trigger that population once
// here so IsBuiltin works without a live interpreter.
var builtinPopulateOnce sync.Once

func IsBuiltin(name string) bool {
	builtinPopulateOnce.Do(func() {
		// Register builtins on a throwaway interpreter so we capture
		// the full name set without having to maintain a parallel list.
		_ = New()
	})
	return builtinNames[name]
}

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

	// --- API introspection ---
	def("openapi", builtinOpenAPI)
	def("routes", builtinRoutesList)
	def("swagger_ui", builtinSwaggerUI)
	def("redoc_ui", builtinRedocUI)
	def("status_page", builtinStatusPage)

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
	def("pp", builtinPP)
	def("eval", builtinEval)
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
	def("markdown", builtinMarkdown)

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
	def("rate_limit", builtinRateLimit)
	def("rate_limit_reset", builtinRateLimitReset)

	// ws.* — outbound WebSocket client. Server-side `ws /chat` blocks
	// already exist; this is for connecting to remote feeds.
	wsNS := NewOrderedMap()
	wsNS.Set("connect", FunctionValue(&Function{Name: "ws.connect", Native: builtinWSConnect}))
	g.Set("ws", ObjectValue(wsNS))
	builtinNames["ws"] = true

	// health.* — k8s-style liveness / readiness probes.
	healthNS := NewOrderedMap()
	healthNS.Set("live", FunctionValue(&Function{Name: "health.live", Native: builtinHealthLive}))
	healthNS.Set("ready", FunctionValue(&Function{Name: "health.ready", Native: builtinHealthReady}))
	g.Set("health", ObjectValue(healthNS))
	builtinNames["health"] = true
	def("random_string", builtinRandomString)
	def("random_bytes", builtinRandomBytes)
	def("base32_encode", builtinBase32Encode)
	def("base32_decode", builtinBase32Decode)
	def("pick", builtinPick)
	def("omit", builtinOmit)
	def("merge", builtinMerge)
	def("deep_merge", builtinDeepMerge)

	// id.* namespace — uuid / ulid / nanoid / short / snowflake
	idNS := NewOrderedMap()
	idNS.Set("uuid", FunctionValue(&Function{Name: "id.uuid", Native: builtinIDUUID}))
	idNS.Set("ulid", FunctionValue(&Function{Name: "id.ulid", Native: builtinIDULID}))
	idNS.Set("nanoid", FunctionValue(&Function{Name: "id.nanoid", Native: builtinIDNanoID}))
	idNS.Set("short", FunctionValue(&Function{Name: "id.short", Native: builtinIDShort}))
	idNS.Set("snowflake", FunctionValue(&Function{Name: "id.snowflake", Native: builtinIDSnowflake}))
	g.Set("id", ObjectValue(idNS))
	builtinNames["id"] = true
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
	def("shell", builtinShell)

	// --- Filesystem watching ---
	fsNS := NewOrderedMap()
	fsNS.Set("watch", FunctionValue(&Function{Name: "fs.watch", Native: builtinFSWatch}))
	g.Set("fs", ObjectValue(fsNS))
	builtinNames["fs"] = true

	// --- Background job queue ---
	jobsNS := NewOrderedMap()
	jobsNS.Set("create", FunctionValue(&Function{Name: "jobs.create", Native: builtinJobsCreate}))
	g.Set("jobs", ObjectValue(jobsNS))
	builtinNames["jobs"] = true

	// Resolve the stderr writer used by jobs.go's worker logger.
	_stderrWrite = func(p []byte) (int, error) { return os.Stderr.Write(p) }

	// Wire crypto/rand for the extra-hashing helpers (argon2 / scrypt).
	crandRead = crand.Read

	// --- CSV ---
	def("csv_parse", builtinCSVParse)
	def("csv_stringify", builtinCSVStringify)

	// --- Printf ---
	def("format", builtinFormat)

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
	def("aes_encrypt", builtinAESEncrypt)
	def("aes_decrypt", builtinAESDecrypt)

	// --- Password namespace ---
	pwNS := NewOrderedMap()
	pwNS.Set("hash", FunctionValue(&Function{Name: "password.hash", Native: builtinPasswordHash}))
	pwNS.Set("verify", FunctionValue(&Function{Name: "password.verify", Native: builtinPasswordVerify}))
	pwNS.Set("hash_argon2", FunctionValue(&Function{Name: "password.hash_argon2", Native: builtinPasswordHashArgon2}))
	pwNS.Set("verify_argon2", FunctionValue(&Function{Name: "password.verify_argon2", Native: builtinPasswordVerifyArgon2}))
	pwNS.Set("hash_scrypt", FunctionValue(&Function{Name: "password.hash_scrypt", Native: builtinPasswordHashScrypt}))
	pwNS.Set("verify_scrypt", FunctionValue(&Function{Name: "password.verify_scrypt", Native: builtinPasswordVerifyScrypt}))

	// --- Passwordless: magic_link namespace ---
	mlNS := NewOrderedMap()
	mlNS.Set("create", FunctionValue(&Function{Name: "magic_link.create", Native: builtinMagicLinkCreate}))
	mlNS.Set("verify", FunctionValue(&Function{Name: "magic_link.verify", Native: builtinMagicLinkVerify}))
	g.Set("magic_link", ObjectValue(mlNS))
	builtinNames["magic_link"] = true

	// --- TOTP (RFC 6238 — Google Authenticator compatible) ---
	totpNS := NewOrderedMap()
	totpNS.Set("generate", FunctionValue(&Function{Name: "totp.generate", Native: builtinTOTPGenerate}))
	totpNS.Set("verify", FunctionValue(&Function{Name: "totp.verify", Native: builtinTOTPVerify}))
	totpNS.Set("uri", FunctionValue(&Function{Name: "totp.uri", Native: builtinTOTPURI}))
	g.Set("totp", ObjectValue(totpNS))
	builtinNames["totp"] = true
	g.Set("password", ObjectValue(pwNS))
	builtinNames["password"] = true

	// --- YAML / TOML ---
	def("yaml_parse", builtinYAMLParse)
	def("yaml_stringify", builtinYAMLStringify)
	def("toml_parse", builtinTOMLParse)
	def("toml_stringify", builtinTOMLStringify)

	// --- Session namespace (signed cookies + claims) ---
	sessionNS := NewOrderedMap()
	sessionNS.Set("create", FunctionValue(&Function{Name: "session.create", Native: builtinSessionCreate}))
	sessionNS.Set("read", FunctionValue(&Function{Name: "session.read", Native: builtinSessionRead}))
	sessionNS.Set("clear", FunctionValue(&Function{Name: "session.clear", Native: builtinSessionClear}))
	g.Set("session", ObjectValue(sessionNS))
	builtinNames["session"] = true

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
	def("time_ago", builtinTimeAgo)
	def("time_human", builtinTimeHuman)

	// --- URL helpers ---
	def("parse_url", builtinParseURL)
	def("url_encode", builtinURLEncode)
	def("url_decode", builtinURLDecode)

	// --- Misc ---
	def("retry", builtinRetry)
	def("assert", builtinAssert)
	def("assert_eq", builtinAssertEq)
	def("validate", builtinValidate)
	def("sign_cookie", builtinSignCookie)
	def("verify_cookie", builtinVerifyCookie)
	def("csrf_token", builtinCSRFToken)
	def("verify_csrf", builtinVerifyCSRF)

	// --- Pub/sub for ws fan-out ---
	pubsubNS := NewOrderedMap()
	pubsubNS.Set("topic", FunctionValue(&Function{Name: "pubsub.topic", Native: builtinPubsubTopic}))
	g.Set("pubsub", ObjectValue(pubsubNS))
	builtinNames["pubsub"] = true
	def("every", builtinEvery)
	def("after", builtinAfter)
	def("debounce", builtinDebounce)
	def("cron", builtinCron)
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

	// --- Redis namespace ---
	redisNS := NewOrderedMap()
	redisNS.Set("connect", FunctionValue(&Function{Name: "redis.connect", Native: builtinRedisConnect}))
	redisNS.Set("set", FunctionValue(&Function{Name: "redis.set", Native: builtinRedisSet}))
	redisNS.Set("get", FunctionValue(&Function{Name: "redis.get", Native: builtinRedisGet}))
	redisNS.Set("del", FunctionValue(&Function{Name: "redis.del", Native: builtinRedisDel}))
	redisNS.Set("incr", FunctionValue(&Function{Name: "redis.incr", Native: builtinRedisIncr}))
	redisNS.Set("expire", FunctionValue(&Function{Name: "redis.expire", Native: builtinRedisExpire}))
	redisNS.Set("publish", FunctionValue(&Function{Name: "redis.publish", Native: builtinRedisPublish}))
	redisNS.Set("close", FunctionValue(&Function{Name: "redis.close", Native: builtinRedisClose}))
	g.Set("redis", ObjectValue(redisNS))
	builtinNames["redis"] = true

	// --- SQL (SQLite) namespace ---
	sqlNS := NewOrderedMap()
	sqlNS.Set("open", FunctionValue(&Function{Name: "sql.open", Native: builtinSQLOpen}))
	sqlNS.Set("exec", FunctionValue(&Function{Name: "sql.exec", Native: builtinSQLExec}))
	sqlNS.Set("query", FunctionValue(&Function{Name: "sql.query", Native: builtinSQLQuery}))
	sqlNS.Set("query_one", FunctionValue(&Function{Name: "sql.query_one", Native: builtinSQLQueryOne}))
	sqlNS.Set("close", FunctionValue(&Function{Name: "sql.close", Native: builtinSQLClose}))
	sqlNS.Set("transaction", FunctionValue(&Function{Name: "sql.transaction", Native: builtinSQLTransaction}))
	sqlNS.Set("migrate", FunctionValue(&Function{Name: "sql.migrate", Native: builtinSQLMigrate}))
	g.Set("sql", ObjectValue(sqlNS))
	builtinNames["sql"] = true

	// --- Image namespace ---
	imageNS := NewOrderedMap()
	imageNS.Set("info", FunctionValue(&Function{Name: "image.info", Native: builtinImageInfo}))
	imageNS.Set("resize", FunctionValue(&Function{Name: "image.resize", Native: builtinImageResize}))
	imageNS.Set("convert", FunctionValue(&Function{Name: "image.convert", Native: builtinImageConvert}))
	imageNS.Set("thumbnail", FunctionValue(&Function{Name: "image.thumbnail", Native: builtinImageThumbnail}))
	imageNS.Set("crop", FunctionValue(&Function{Name: "image.crop", Native: builtinImageCrop}))
	g.Set("image", ObjectValue(imageNS))
	builtinNames["image"] = true

	// --- OAuth2 namespace ---
	oauthNS := NewOrderedMap()
	oauthNS.Set("authorize_url", FunctionValue(&Function{Name: "oauth.authorize_url", Native: builtinOAuthAuthorizeURL}))
	oauthNS.Set("exchange_code", FunctionValue(&Function{Name: "oauth.exchange_code", Native: builtinOAuthExchangeCode}))
	g.Set("oauth", ObjectValue(oauthNS))
	builtinNames["oauth"] = true

	// --- Concurrency: channels ---
	def("chan", builtinChan)
	def("send", builtinChanSend)
	def("recv", builtinChanRecv)
	def("close_chan", builtinChanClose)
	def("wait_group", builtinWaitGroup)

	// --- time / path / fs namespaces ---
	timeNS := NewOrderedMap()
	timeNS.Set("now", FunctionValue(&Function{Name: "time.now", Native: builtinTimeNow}))
	timeNS.Set("parse", FunctionValue(&Function{Name: "time.parse", Native: builtinTimeParse}))
	timeNS.Set("unix", FunctionValue(&Function{Name: "time.unix", Native: builtinTimeUnix}))
	timeNS.Set("format", FunctionValue(&Function{Name: "time.format", Native: builtinTimeFormat}))
	timeNS.Set("iso", FunctionValue(&Function{Name: "time.iso", Native: builtinTimeISO}))
	timeNS.Set("add", FunctionValue(&Function{Name: "time.add", Native: builtinTimeAdd}))
	timeNS.Set("diff", FunctionValue(&Function{Name: "time.diff", Native: builtinTimeDiff}))
	timeNS.Set("weekday", FunctionValue(&Function{Name: "time.weekday", Native: builtinTimeWeekday}))
	timeNS.Set("year", FunctionValue(&Function{Name: "time.year", Native: builtinTimeYear}))
	timeNS.Set("month", FunctionValue(&Function{Name: "time.month", Native: builtinTimeMonth}))
	timeNS.Set("day", FunctionValue(&Function{Name: "time.day", Native: builtinTimeDay}))
	timeNS.Set("hour", FunctionValue(&Function{Name: "time.hour", Native: builtinTimeHour}))
	timeNS.Set("minute", FunctionValue(&Function{Name: "time.minute", Native: builtinTimeMinute}))
	timeNS.Set("second", FunctionValue(&Function{Name: "time.second", Native: builtinTimeSecond}))
	g.Set("time", ObjectValue(timeNS))
	builtinNames["time"] = true

	pathNS := NewOrderedMap()
	pathNS.Set("join", FunctionValue(&Function{Name: "path.join", Native: builtinPathJoin}))
	pathNS.Set("dir", FunctionValue(&Function{Name: "path.dir", Native: builtinPathDir}))
	pathNS.Set("base", FunctionValue(&Function{Name: "path.base", Native: builtinPathBase}))
	pathNS.Set("ext", FunctionValue(&Function{Name: "path.ext", Native: builtinPathExt}))
	pathNS.Set("absolute", FunctionValue(&Function{Name: "path.absolute", Native: builtinPathAbsolute}))
	g.Set("path", ObjectValue(pathNS))
	builtinNames["path"] = true

	// fs.glob lives next to the existing fs.watch.
	if existingFS, ok := g.Get("fs"); ok && existingFS.Kind == KindObject {
		existingFS.Object.Set("glob", FunctionValue(&Function{Name: "fs.glob", Native: builtinFSGlob}))
	} else {
		fsNS := NewOrderedMap()
		fsNS.Set("glob", FunctionValue(&Function{Name: "fs.glob", Native: builtinFSGlob}))
		g.Set("fs", ObjectValue(fsNS))
		builtinNames["fs"] = true
	}

	// --- Metrics namespace ---
	// Prometheus-compatible counters / gauges / histograms with an
	// auto-rendered exposition handler. Drop into any monitoring
	// stack that scrapes openmetrics text.
	metricsNS := NewOrderedMap()
	metricsNS.Set("counter", FunctionValue(&Function{Name: "metrics.counter", Native: builtinMetricsCounter}))
	metricsNS.Set("gauge", FunctionValue(&Function{Name: "metrics.gauge", Native: builtinMetricsGauge}))
	metricsNS.Set("histogram", FunctionValue(&Function{Name: "metrics.histogram", Native: builtinMetricsHistogram}))
	metricsNS.Set("text", FunctionValue(&Function{Name: "metrics.text", Native: builtinMetricsText}))
	metricsNS.Set("handler", FunctionValue(&Function{Name: "metrics.handler", Native: builtinMetricsHandler}))
	metricsNS.Set("reset", FunctionValue(&Function{Name: "metrics.reset", Native: builtinMetricsReset}))
	g.Set("metrics", ObjectValue(metricsNS))
	builtinNames["metrics"] = true

	// --- S3 namespace ---
	// Pure-Go AWS Signature V4 over the standard S3 wire format.
	// Works with AWS S3, Cloudflare R2, Backblaze B2, DigitalOcean
	// Spaces, MinIO, Wasabi — anything that speaks the protocol.
	s3NS := NewOrderedMap()
	s3NS.Set("put", FunctionValue(&Function{Name: "s3.put", Native: builtinS3Put}))
	s3NS.Set("get", FunctionValue(&Function{Name: "s3.get", Native: builtinS3Get}))
	s3NS.Set("delete", FunctionValue(&Function{Name: "s3.delete", Native: builtinS3Delete}))
	s3NS.Set("list", FunctionValue(&Function{Name: "s3.list", Native: builtinS3List}))
	s3NS.Set("presign", FunctionValue(&Function{Name: "s3.presign", Native: builtinS3Presign}))
	g.Set("s3", ObjectValue(s3NS))
	builtinNames["s3"] = true

	// --- Search namespace (SQLite FTS5) ---
	searchNS := NewOrderedMap()
	searchNS.Set("create", FunctionValue(&Function{Name: "search.create", Native: builtinSearchCreate}))
	searchNS.Set("index", FunctionValue(&Function{Name: "search.index", Native: builtinSearchIndex}))
	searchNS.Set("query", FunctionValue(&Function{Name: "search.query", Native: builtinSearchQuery}))
	searchNS.Set("delete", FunctionValue(&Function{Name: "search.delete", Native: builtinSearchDelete}))
	g.Set("search", ObjectValue(searchNS))
	builtinNames["search"] = true

	// --- Stripe namespace ---
	// Thin wrappers over the four Stripe API calls SaaS apps actually
	// need: checkout sessions, customers, the billing portal, and
	// subscriptions. All read STRIPE_SECRET_KEY from the environment.
	stripeNS := NewOrderedMap()
	stripeNS.Set("checkout", FunctionValue(&Function{Name: "stripe.checkout", Native: builtinStripeCheckout}))
	stripeNS.Set("customer_create", FunctionValue(&Function{Name: "stripe.customer_create", Native: builtinStripeCustomerCreate}))
	stripeNS.Set("customer_portal", FunctionValue(&Function{Name: "stripe.customer_portal", Native: builtinStripeCustomerPortal}))
	stripeNS.Set("subscription_create", FunctionValue(&Function{Name: "stripe.subscription_create", Native: builtinStripeSubscriptionCreate}))
	g.Set("stripe", ObjectValue(stripeNS))
	builtinNames["stripe"] = true

	// --- Webhooks namespace ---
	// Production webhook senders sign payloads in subtly different ways:
	// HMAC-SHA256, with or without a timestamp prefix, hex or base64,
	// header-encoded with extra metadata. The webhooks namespace handles
	// each one in a single call so route handlers don't ship hand-rolled
	// crypto.
	webhooks := NewOrderedMap()
	webhooks.Set("verify_stripe", FunctionValue(&Function{Name: "webhooks.verify_stripe", Native: builtinWebhooksVerifyStripe}))
	webhooks.Set("verify_github", FunctionValue(&Function{Name: "webhooks.verify_github", Native: builtinWebhooksVerifyGitHub}))
	webhooks.Set("verify_svix", FunctionValue(&Function{Name: "webhooks.verify_svix", Native: builtinWebhooksVerifySvix}))
	webhooks.Set("verify_shopify", FunctionValue(&Function{Name: "webhooks.verify_shopify", Native: builtinWebhooksVerifyShopify}))
	webhooks.Set("verify_slack", FunctionValue(&Function{Name: "webhooks.verify_slack", Native: builtinWebhooksVerifySlack}))
	g.Set("webhooks", ObjectValue(webhooks))
	builtinNames["webhooks"] = true

	// --- Notify namespace ---
	// notify.* are one-shot outbound posts to common SaaS endpoints.
	// Each returns { ok, status, error } so callers can stay
	// declarative ("if (!r.ok) ...") without try/catch boilerplate.
	notifyNS := NewOrderedMap()
	notifyNS.Set("slack", FunctionValue(&Function{Name: "notify.slack", Native: builtinNotifySlack}))
	notifyNS.Set("discord", FunctionValue(&Function{Name: "notify.discord", Native: builtinNotifyDiscord}))
	notifyNS.Set("email", FunctionValue(&Function{Name: "notify.email", Native: builtinNotifyEmail}))
	g.Set("notify", ObjectValue(notifyNS))
	builtinNames["notify"] = true

	// --- AI namespace ---
	ai := NewOrderedMap()
	ai.Set("complete", FunctionValue(&Function{Name: "ai.complete", Native: builtinAIComplete}))
	ai.Set("embed", FunctionValue(&Function{Name: "ai.embed", Native: builtinAIEmbed}))
	ai.Set("stream", FunctionValue(&Function{Name: "ai.stream", Native: builtinAIStream}))
	ai.Set("vision", FunctionValue(&Function{Name: "ai.vision", Native: builtinAIVision}))
	ai.Set("similarity", FunctionValue(&Function{Name: "ai.similarity", Native: builtinAISimilarity}))
	ai.Set("image", FunctionValue(&Function{Name: "ai.image", Native: builtinAIImage}))
	ai.Set("transcribe", FunctionValue(&Function{Name: "ai.transcribe", Native: builtinAITranscribe}))
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
	fmt.Fprintln(i.Out, strings.Join(parts, " "))
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
	fmt.Fprint(i.Out, strings.Join(parts, " "))
	return NullValue(), nil
}

func builtinEprint(i *Interpreter, args []Value) (Value, error) {
	parts := make([]string, len(args))
	for k, a := range args {
		parts[k] = a.Display()
	}
	fmt.Fprintln(i.Err, strings.Join(parts, " "))
	return NullValue(), nil
}

// ===== API introspection =====

// openapi(info?) returns an OpenAPI 3.1 spec object. The info argument
// optionally overrides title / version / description / contact. Path
// params are auto-extracted (`/users/:id` becomes `/users/{id}` with a
// matching parameter declaration).
//
//	get /openapi.json {
//	  return json(openapi({ title: "My API", version: "1.0" }))
//	}
func builtinOpenAPI(i *Interpreter, args []Value) (Value, error) {
	info := NewOrderedMap()
	info.Set("title", StringValue("MX Script API"))
	info.Set("version", StringValue("1.0.0"))
	if len(args) > 0 && args[0].Kind == KindObject {
		for _, k := range args[0].Object.Keys {
			v, _ := args[0].Object.Get(k)
			info.Set(k, v)
		}
	}

	paths := NewOrderedMap()
	for _, r := range i.routes {
		method := strings.ToLower(r.Method)
		// SSE / WS routes describe themselves as GET in OpenAPI; they
		// share the same Upgrade-from-GET contract.
		if method == "sse" || method == "ws" {
			method = "get"
		}

		// Convert /users/:id to /users/{id} and collect parameters.
		var pathOut strings.Builder
		var params []Value
		pathOut.WriteByte('/')
		for k, part := range r.PathParts {
			if k > 0 {
				pathOut.WriteByte('/')
			}
			if strings.HasPrefix(part, ":") {
				name := part[1:]
				pathOut.WriteByte('{')
				pathOut.WriteString(name)
				pathOut.WriteByte('}')
				p := NewOrderedMap()
				p.Set("name", StringValue(name))
				p.Set("in", StringValue("path"))
				p.Set("required", BoolValue(true))
				schema := NewOrderedMap()
				schema.Set("type", StringValue("string"))
				p.Set("schema", ObjectValue(schema))
				params = append(params, ObjectValue(p))
			} else {
				pathOut.WriteString(part)
			}
		}
		pathStr := pathOut.String()
		if pathStr == "/" && len(r.PathParts) == 0 {
			pathStr = "/"
		}

		// Build the operation object.
		op := NewOrderedMap()
		op.Set("summary", StringValue(strings.ToUpper(r.Method)+" "+pathStr))
		if len(params) > 0 {
			op.Set("parameters", ArrayValue(params))
		}
		responses := NewOrderedMap()
		ok := NewOrderedMap()
		ok.Set("description", StringValue("OK"))
		responses.Set("200", ObjectValue(ok))
		op.Set("responses", ObjectValue(responses))
		op.Set("tags", ArrayValue([]Value{StringValue("default")}))

		// Path may already exist (multiple methods on same path).
		entry, exists := paths.Get(pathStr)
		var item *OrderedMap
		if exists && entry.Kind == KindObject {
			item = entry.Object
		} else {
			item = NewOrderedMap()
		}
		item.Set(method, ObjectValue(op))
		paths.Set(pathStr, ObjectValue(item))
	}

	out := NewOrderedMap()
	out.Set("openapi", StringValue("3.1.0"))
	out.Set("info", ObjectValue(info))
	out.Set("paths", ObjectValue(paths))
	return ObjectValue(out), nil
}

// swagger_ui(spec_url, opts?) returns an HTML response that renders an
// interactive Swagger UI for the given OpenAPI spec. Combine with
// `openapi()` and a `/openapi.json` route for instant browsable API
// docs:
//
//	get /openapi.json { return json(openapi({ title: "My API" })) }
//	get /docs         { return swagger_ui("/openapi.json") }
func builtinSwaggerUI(i *Interpreter, args []Value) (Value, error) {
	specURL := "/openapi.json"
	if len(args) > 0 && args[0].Kind == KindString {
		specURL = args[0].String
	}
	title := "API docs"
	if len(args) > 1 && args[1].Kind == KindObject {
		if v, ok := args[1].Object.Get("title"); ok && v.Kind == KindString {
			title = v.String
		}
	}
	html := `<!doctype html>
<html><head><meta charset=utf-8><title>` + htmlEscapeString(title) + `</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>body{margin:0}</style></head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  window.onload = () => SwaggerUIBundle({
    url: '` + specURL + `',
    dom_id: '#swagger-ui',
    deepLinking: true,
    layout: 'BaseLayout'
  });
</script>
</body></html>`
	return ResponseValue(&Response{ContentType: "text/html; charset=utf-8", Body: StringValue(html)}), nil
}

// redoc_ui(spec_url, opts?) — same idea but using Redoc, which produces
// a much cleaner reference-style layout for read-only consumers.
func builtinRedocUI(i *Interpreter, args []Value) (Value, error) {
	specURL := "/openapi.json"
	if len(args) > 0 && args[0].Kind == KindString {
		specURL = args[0].String
	}
	title := "API reference"
	if len(args) > 1 && args[1].Kind == KindObject {
		if v, ok := args[1].Object.Get("title"); ok && v.Kind == KindString {
			title = v.String
		}
	}
	html := `<!doctype html>
<html><head><meta charset=utf-8><title>` + htmlEscapeString(title) + `</title>
<style>body{margin:0}</style></head>
<body>
<redoc spec-url="` + specURL + `"></redoc>
<script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body></html>`
	return ResponseValue(&Response{ContentType: "text/html; charset=utf-8", Body: StringValue(html)}), nil
}

// status_page(opts?) returns an HTML response with a small status
// dashboard: app name, uptime (computed from server start), Go runtime,
// route count, route table. Mounting it costs one route:
//
//	get /status { return status_page({ app: "My API" }) }
func builtinStatusPage(i *Interpreter, args []Value) (Value, error) {
	app := "MX Script app"
	if len(args) > 0 && args[0].Kind == KindObject {
		if v, ok := args[0].Object.Get("app"); ok && v.Kind == KindString {
			app = v.String
		}
	}
	uptime := time.Since(serverStartTime)
	var rows strings.Builder
	for _, r := range i.routes {
		path := "/" + strings.Join(r.PathParts, "/")
		fmt.Fprintf(&rows, `<tr><td><span class=method m-%s>%s</span></td><td><code>%s</code></td></tr>`,
			strings.ToLower(r.Method), r.Method, htmlEscapeString(path))
	}
	html := `<!doctype html><html><head><meta charset=utf-8><title>` + htmlEscapeString(app) + ` · status</title>
<style>
  body { font: 14px/1.5 -apple-system,sans-serif; max-width: 720px; margin: 40px auto; padding: 0 20px; color: #1d2333; }
  h1 { margin: 0 0 8px; font-weight: 600; }
  .sub { color: #5a6072; margin: 0 0 32px; font-size: 13px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 12px; margin-bottom: 32px; }
  .card { padding: 14px 16px; background: #f6f7fb; border-radius: 8px; }
  .card .label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; color: #5a6072; }
  .card .value { font-size: 18px; font-weight: 600; margin-top: 2px; }
  .ok { color: #16a34a; }
  table { width: 100%; border-collapse: collapse; }
  td { padding: 8px 12px; border-bottom: 1px solid #e5e7ef; }
  td code { font-family: ui-monospace, monospace; font-size: 13px; }
  .method { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; color: #fff; }
  .m-get { background: #2752ba; } .m-post { background: #16a34a; } .m-put { background: #d97706; }
  .m-delete { background: #dc2626; } .m-patch { background: #7c3aed; }
  .m-sse, .m-ws { background: #0891b2; }
</style></head><body>
<h1>` + htmlEscapeString(app) + ` <span class=ok>●</span></h1>
<p class=sub>Status as of ` + time.Now().UTC().Format(time.RFC3339) + `</p>
<div class=grid>
  <div class=card><div class=label>uptime</div><div class=value>` + humanDuration(uptime) + `</div></div>
  <div class=card><div class=label>routes</div><div class=value>` + strconv.Itoa(len(i.routes)) + `</div></div>
  <div class=card><div class=label>statics</div><div class=value>` + strconv.Itoa(len(i.statics)) + `</div></div>
  <div class=card><div class=label>middlewares</div><div class=value>` + strconv.Itoa(len(i.middlewares)) + `</div></div>
</div>
<table>` + rows.String() + `</table>
</body></html>`
	return ResponseValue(&Response{ContentType: "text/html; charset=utf-8", Body: StringValue(html)}), nil
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours()/24), int(d.Hours())%24)
	}
}

// routes() returns an array of { method, path } objects for every
// registered route — useful for ad-hoc introspection / status pages.
func builtinRoutesList(i *Interpreter, args []Value) (Value, error) {
	out := make([]Value, 0, len(i.routes))
	for _, r := range i.routes {
		o := NewOrderedMap()
		o.Set("method", StringValue(r.Method))
		o.Set("path", StringValue("/"+strings.Join(r.PathParts, "/")))
		out = append(out, ObjectValue(o))
	}
	return ArrayValue(out), nil
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

// markdown renders a small subset of CommonMark to safe HTML. Supports
// headings, paragraphs, bold (**), italic (*), inline code (`), links
// ([text](url)), lists (- and 1.), block code fences (```), and blank-line
// separated paragraphs. All text is HTML-escaped before formatting so it's
// safe for untrusted input. Not a full CommonMark implementation — for
// rich features pull in a community library.
func builtinMarkdown(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(renderMarkdown(s)), nil
}

func renderMarkdown(s string) string {
	var out strings.Builder
	lines := strings.Split(s, "\n")
	inCode := false
	inUL := false
	inOL := false
	var paraBuf []string

	flushPara := func() {
		if len(paraBuf) == 0 {
			return
		}
		joined := strings.Join(paraBuf, " ")
		out.WriteString("<p>")
		out.WriteString(renderInline(joined))
		out.WriteString("</p>\n")
		paraBuf = nil
	}
	closeLists := func() {
		if inUL {
			out.WriteString("</ul>\n")
			inUL = false
		}
		if inOL {
			out.WriteString("</ol>\n")
			inOL = false
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			flushPara()
			closeLists()
			if !inCode {
				out.WriteString("<pre><code>")
				inCode = true
			} else {
				out.WriteString("</code></pre>\n")
				inCode = false
			}
			continue
		}
		if inCode {
			out.WriteString(htmlEscapeString(line))
			out.WriteByte('\n')
			continue
		}

		if trimmed == "" {
			flushPara()
			closeLists()
			continue
		}

		// Headings
		if strings.HasPrefix(trimmed, "#") {
			flushPara()
			closeLists()
			level := 0
			for level < 6 && level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			text := strings.TrimSpace(trimmed[level:])
			fmt.Fprintf(&out, "<h%d>%s</h%d>\n", level, renderInline(text), level)
			continue
		}

		// Unordered list
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			flushPara()
			if !inUL {
				closeLists()
				out.WriteString("<ul>\n")
				inUL = true
			}
			fmt.Fprintf(&out, "  <li>%s</li>\n", renderInline(trimmed[2:]))
			continue
		}

		// Ordered list (very loose: starts with digit + ".")
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			dot := strings.Index(trimmed, ". ")
			if dot > 0 {
				flushPara()
				if !inOL {
					closeLists()
					out.WriteString("<ol>\n")
					inOL = true
				}
				fmt.Fprintf(&out, "  <li>%s</li>\n", renderInline(trimmed[dot+2:]))
				continue
			}
		}

		closeLists()
		paraBuf = append(paraBuf, trimmed)
	}
	flushPara()
	closeLists()
	if inCode {
		out.WriteString("</code></pre>\n")
	}
	return out.String()
}

// renderInline handles **bold**, *italic*, `code`, [text](url) inside a
// run of body text. Escapes everything else first to keep XSS safe.
func renderInline(s string) string {
	s = htmlEscapeString(s)
	// `code`
	s = inlineWrap(s, "`", "<code>", "</code>")
	// **bold**
	s = inlineWrap(s, "**", "<strong>", "</strong>")
	// *italic*
	s = inlineWrap(s, "*", "<em>", "</em>")
	// [text](url)
	s = inlineLinks(s)
	return s
}

func inlineWrap(s, delim, open, close string) string {
	var out strings.Builder
	in := false
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], delim) {
			if in {
				out.WriteString(close)
			} else {
				out.WriteString(open)
			}
			in = !in
			i += len(delim)
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func inlineLinks(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '[' {
			closeBracket := strings.Index(s[i:], "]")
			if closeBracket > 0 && i+closeBracket+1 < len(s) && s[i+closeBracket+1] == '(' {
				closeParen := strings.Index(s[i+closeBracket+1:], ")")
				if closeParen > 0 {
					text := s[i+1 : i+closeBracket]
					url := s[i+closeBracket+2 : i+closeBracket+1+closeParen]
					fmt.Fprintf(&out, `<a href="%s">%s</a>`, url, text)
					i = i + closeBracket + 1 + closeParen + 1
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
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

// random_string(n, alphabet?) returns an n-character string drawn at
// random from `alphabet`. Default alphabet is RFC 4648 base32 — the
// shape TOTP secrets and short identifiers want.
func builtinRandomString(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	if len(args) > 1 && args[1].Kind == KindString {
		alphabet = args[1].String
	}
	if alphabet == "" || n <= 0 {
		return StringValue(""), nil
	}
	out := make([]byte, int(n))
	buf := make([]byte, int(n))
	if _, err := crand.Read(buf); err != nil {
		return Value{}, fmt.Errorf("random_string: %w", err)
	}
	for k := range out {
		out[k] = alphabet[int(buf[k])%len(alphabet)]
	}
	return StringValue(string(out)), nil
}

// random_bytes(n) returns n cryptographically random bytes hex-encoded
// (so the result is a 2n-character string). Use base64_encode or
// base32_encode if you need a different format.
func builtinRandomBytes(i *Interpreter, args []Value) (Value, error) {
	n, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	if n <= 0 {
		return StringValue(""), nil
	}
	buf := make([]byte, int(n))
	if _, err := crand.Read(buf); err != nil {
		return Value{}, fmt.Errorf("random_bytes: %w", err)
	}
	return StringValue(hex.EncodeToString(buf)), nil
}

// base32_encode(s) returns the RFC 4648 base32 encoding of `s` (with
// padding). Mirrors base64_encode for users who need authenticator-
// app–compatible secrets or DNS-safe identifiers.
func builtinBase32Encode(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(base32.StdEncoding.EncodeToString([]byte(s))), nil
}

// base32_decode(s) reverses base32_encode. Tolerates lower-case input
// and missing trailing padding.
func builtinBase32Decode(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	clean := strings.ToUpper(strings.TrimSpace(s))
	for len(clean)%8 != 0 {
		clean += "="
	}
	out, err := base32.StdEncoding.DecodeString(clean)
	if err != nil {
		return Value{}, fmt.Errorf("base32_decode: %w", err)
	}
	return StringValue(string(out)), nil
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

// ===== Background job queue =====
//
// jobs.create({ db, queue, max_attempts }) returns a queue object with
// methods .enqueue / .process / .close / .stats. Implementation lives
// in jobs.go (gated by `!js`) with stubs in jobs_wasm.go.

// ===== Filesystem watching =====
//
// fs.watch(path, fn, opts?) polls a path (file or directory) for
// changes — every 500ms by default — and calls fn(event) when something
// shifts. Each event is { path, kind } where kind is "added" / "modified"
// / "removed". Returns a stop function. Pure stdlib (no fsnotify dep);
// good enough for hot-reload / build-watch use cases.
//
//	let stop = fs.watch("./public", fn(ev) {
//	  print("[" + ev.kind + "]", ev.path)
//	})
//	// later: stop()

func builtinFSWatch(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("fs.watch(path, fn, opts?) requires (string, function)")
	}
	root := args[0].String
	fn := args[1].Function
	interval := 500 * time.Millisecond
	if len(args) > 2 && args[2].Kind == KindObject {
		if v, ok := args[2].Object.Get("interval_ms"); ok && v.Kind == KindNumber {
			interval = time.Duration(v.Number) * time.Millisecond
		}
	}

	stop := make(chan struct{})
	go func() {
		prev := snapshotFiles(root)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				cur := snapshotFiles(root)
				for path, mod := range cur {
					if old, ok := prev[path]; !ok {
						fireFSEvent(i, fn, "added", path)
					} else if old != mod {
						fireFSEvent(i, fn, "modified", path)
					}
				}
				for path := range prev {
					if _, ok := cur[path]; !ok {
						fireFSEvent(i, fn, "removed", path)
					}
				}
				prev = cur
			}
		}
	}()
	cancel := &Function{Name: "stop", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		select {
		case <-stop:
		default:
			close(stop)
		}
		return NullValue(), nil
	}}
	return FunctionValue(cancel), nil
}

func snapshotFiles(root string) map[string]int64 {
	out := map[string]int64{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		// Combine size + mtime so renames-with-same-name still register.
		out[path] = info.Size()*1_000_000_000 + info.ModTime().UnixNano()%1_000_000_000
		return nil
	})
	return out
}

func fireFSEvent(i *Interpreter, fn *Function, kind, path string) {
	ev := NewOrderedMap()
	ev.Set("kind", StringValue(kind))
	ev.Set("path", StringValue(path))
	if _, err := i.callFunction(nil, fn, []Value{ObjectValue(ev)}); err != nil {
		fmt.Fprintf(os.Stderr, "[mx fs.watch] %v\n", err)
	}
}

// ===== Subprocess =====
//
// shell(cmd, args?, opts?) runs an OS command and returns
// { stdout, stderr, exit_code }. Helpful for scripting use cases —
// devops glue, build tools, anything that previously meant dropping
// to bash.
//
//	let r = shell("git", ["log", "-1", "--format=%H"])
//	if (r.exit_code != 0) { error("git failed: " + r.stderr) }
//	print("HEAD:", trim(r.stdout))
//
// opts may include `dir` (cwd), `env` (object of overrides), `stdin`
// (string piped to the process), and `timeout_ms`.

func builtinShell(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("shell(cmd, args?, opts?) requires a string command")
	}
	cmdName := args[0].String
	var argv []string
	if len(args) > 1 && args[1].Kind == KindArray {
		argv = stringsFromArray(args[1].Array)
	}

	var dir string
	var stdin string
	envOverrides := map[string]string{}
	timeout := 30 * time.Second
	if len(args) > 2 && args[2].Kind == KindObject {
		o := args[2].Object
		if v, ok := o.Get("dir"); ok && v.Kind == KindString {
			dir = v.String
		}
		if v, ok := o.Get("stdin"); ok && v.Kind == KindString {
			stdin = v.String
		}
		if v, ok := o.Get("timeout_ms"); ok && v.Kind == KindNumber {
			timeout = time.Duration(v.Number) * time.Millisecond
		}
		if v, ok := o.Get("env"); ok && v.Kind == KindObject {
			for _, k := range v.Object.Keys {
				if val, _ := v.Object.Get(k); val.Kind == KindString {
					envOverrides[k] = val.String
				}
			}
		}
	}

	ctx, cancel := contextWithTimeout(timeout)
	defer cancel()
	c := exec.CommandContext(ctx, cmdName, argv...)
	if dir != "" {
		c.Dir = dir
	}
	if len(envOverrides) > 0 {
		base := os.Environ()
		for k, v := range envOverrides {
			base = append(base, k+"="+v)
		}
		c.Env = base
	}
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	err := c.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return Value{}, err
		}
	}
	out := NewOrderedMap()
	out.Set("stdout", StringValue(outBuf.String()))
	out.Set("stderr", StringValue(errBuf.String()))
	out.Set("exit_code", NumberValue(float64(exitCode)))
	return ObjectValue(out), nil
}

// ===== CSV =====

func builtinCSVParse(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	r := csv.NewReader(strings.NewReader(s))
	r.FieldsPerRecord = -1 // tolerate ragged rows
	rows, err := r.ReadAll()
	if err != nil {
		return Value{}, err
	}
	out := make([]Value, len(rows))
	for i, row := range rows {
		cells := make([]Value, len(row))
		for j, c := range row {
			cells[j] = StringValue(c)
		}
		out[i] = ArrayValue(cells)
	}
	return ArrayValue(out), nil
}

func builtinCSVStringify(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("csv_stringify(rows) requires an array of arrays")
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for _, row := range args[0].Array {
		if row.Kind != KindArray {
			return Value{}, fmt.Errorf("csv_stringify: each row must be an array")
		}
		fields := make([]string, len(row.Array))
		for k, c := range row.Array {
			fields[k] = c.Display()
		}
		if err := w.Write(fields); err != nil {
			return Value{}, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return Value{}, err
	}
	return StringValue(buf.String()), nil
}

// ===== Printf-style format =====

// format(fmtStr, ...args) — minimal printf clone. Supports %s, %d, %f,
// %v (any). MX Script users mostly want string interpolation; format
// shows up in log lines and CSV rows.
func builtinFormat(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("format(fmt, ...args) requires a format string")
	}
	rest := make([]any, 0, len(args)-1)
	for _, a := range args[1:] {
		switch a.Kind {
		case KindNumber:
			if a.Number == float64(int64(a.Number)) {
				rest = append(rest, int64(a.Number))
			} else {
				rest = append(rest, a.Number)
			}
		case KindString:
			rest = append(rest, a.String)
		case KindBool:
			rest = append(rest, a.Bool)
		default:
			rest = append(rest, a.Display())
		}
	}
	return StringValue(fmt.Sprintf(args[0].String, rest...)), nil
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

// ===== Password hashing (PBKDF2-SHA256) =====
//
// Format on disk: "pbkdf2-sha256$<iterations>$<salt-hex>$<hash-hex>"
//
// PBKDF2 is NIST-approved, supported by OWASP, and implementable in
// stdlib. Bcrypt would require x/crypto; we keep MX dep-light.

const (
	pbkdf2Iterations = 100_000
	pbkdf2KeyLen     = 32
	pbkdf2SaltLen    = 16
)

func pbkdf2Hash(password string, salt []byte, iter, keyLen int) []byte {
	hLen := sha256.Size
	numBlocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, numBlocks*hLen)
	for block := 1; block <= numBlocks; block++ {
		U := make([]byte, len(salt)+4)
		copy(U, salt)
		U[len(salt)] = byte(block >> 24)
		U[len(salt)+1] = byte(block >> 16)
		U[len(salt)+2] = byte(block >> 8)
		U[len(salt)+3] = byte(block)
		mac := hmac.New(sha256.New, []byte(password))
		mac.Write(U)
		T := mac.Sum(nil)
		U = T
		for i := 1; i < iter; i++ {
			mac.Reset()
			mac.Write(U)
			U = mac.Sum(nil)
			for j := range T {
				T[j] ^= U[j]
			}
		}
		out = append(out, T...)
	}
	return out[:keyLen]
}

func builtinPasswordHash(i *Interpreter, args []Value) (Value, error) {
	password, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := crand.Read(salt); err != nil {
		return Value{}, err
	}
	hash := pbkdf2Hash(password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	return StringValue(fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		pbkdf2Iterations, hex.EncodeToString(salt), hex.EncodeToString(hash))), nil
}

func builtinPasswordVerify(i *Interpreter, args []Value) (Value, error) {
	password, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	stored, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return BoolValue(false), nil
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return BoolValue(false), nil
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return BoolValue(false), nil
	}
	want, err := hex.DecodeString(parts[3])
	if err != nil {
		return BoolValue(false), nil
	}
	got := pbkdf2Hash(password, salt, iter, len(want))
	return BoolValue(hmac.Equal(got, want)), nil
}

// ===== Session helper =====
//
// session.create(claims, opts) returns a Response that sets a signed
// session cookie. session.read(request, secret?) reads + verifies it.
// session.clear() returns a Response that expires the cookie.
//
//	post /login {
//	  // ... validate creds ...
//	  return session.create({ user_id: 42, role: "admin" }, {
//	    secret: env_required("SESSION_SECRET"),
//	    max_age: 86400  // 1 day, defaults to 30 days
//	  })
//	}
//
//	get /me {
//	  let claims = session.read(request, env("SESSION_SECRET"))
//	  if (claims == null) { return status(401) }
//	  return json(claims)
//	}
//
//	post /logout { return session.clear() }

const sessionCookieName = "mx_session"

func sessionCookieOpts(opts *OrderedMap) (name string, maxAge int, path, domain, sameSite string, httpOnly, secure bool) {
	name = sessionCookieName
	maxAge = 30 * 86400
	path = "/"
	httpOnly = true
	sameSite = "Lax"
	if opts == nil {
		return
	}
	if v, ok := opts.Get("name"); ok && v.Kind == KindString {
		name = v.String
	}
	if v, ok := opts.Get("max_age"); ok && v.Kind == KindNumber {
		maxAge = int(v.Number)
	}
	if v, ok := opts.Get("path"); ok && v.Kind == KindString {
		path = v.String
	}
	if v, ok := opts.Get("domain"); ok && v.Kind == KindString {
		domain = v.String
	}
	if v, ok := opts.Get("same_site"); ok && v.Kind == KindString {
		sameSite = v.String
	}
	if v, ok := opts.Get("http_only"); ok && v.Kind == KindBool {
		httpOnly = v.Bool
	}
	if v, ok := opts.Get("secure"); ok && v.Kind == KindBool {
		secure = v.Bool
	}
	return
}

func builtinSessionCreate(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("session.create(claims, opts) requires a claims object")
	}
	if len(args) < 2 || args[1].Kind != KindObject {
		return Value{}, fmt.Errorf("session.create(claims, opts) requires opts.secret")
	}
	opts := args[1].Object
	secret := ""
	if v, ok := opts.Get("secret"); ok && v.Kind == KindString {
		secret = v.String
	}
	if secret == "" {
		return Value{}, fmt.Errorf("session.create requires opts.secret")
	}

	// Encode claims as JSON, then sign HMAC-SHA256 over base64(json).
	claimsJSON, err := jsonEncode(args[0])
	if err != nil {
		return Value{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	signed := payload + "." + sig

	name, maxAge, path, domain, sameSite, httpOnly, secure := sessionCookieOpts(opts)
	c := &http.Cookie{
		Name: name, Value: signed, Path: path, Domain: domain,
		MaxAge: maxAge, HttpOnly: httpOnly, Secure: secure,
	}
	switch strings.ToLower(sameSite) {
	case "strict":
		c.SameSite = http.SameSiteStrictMode
	case "lax":
		c.SameSite = http.SameSiteLaxMode
	case "none":
		c.SameSite = http.SameSiteNoneMode
	}

	body := args[0]
	if v, ok := opts.Get("body"); ok {
		body = v
	}
	resp := &Response{ContentType: "application/json", Body: body, Cookies: []*http.Cookie{c}}
	return ResponseValue(resp), nil
}

func builtinSessionRead(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("session.read(request, secret) requires (request, secret)")
	}
	if args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("session.read: first arg must be `request`")
	}
	secret, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	cookieName := sessionCookieName
	if len(args) > 2 && args[2].Kind == KindString {
		cookieName = args[2].String
	}
	cookies, _ := args[0].Object.Get("cookies")
	if cookies.Kind != KindObject {
		return NullValue(), nil
	}
	val, _ := cookies.Object.Get(cookieName)
	if val.Kind != KindString {
		return NullValue(), nil
	}
	parts := strings.SplitN(val.String, ".", 2)
	if len(parts) != 2 {
		return NullValue(), nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return NullValue(), nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return NullValue(), nil
	}
	claims, err := jsonDecode(raw)
	if err != nil {
		return NullValue(), nil
	}
	return claims, nil
}

func builtinSessionClear(i *Interpreter, args []Value) (Value, error) {
	name := sessionCookieName
	path := "/"
	if len(args) > 0 && args[0].Kind == KindObject {
		if v, ok := args[0].Object.Get("name"); ok && v.Kind == KindString {
			name = v.String
		}
		if v, ok := args[0].Object.Get("path"); ok && v.Kind == KindString {
			path = v.String
		}
	}
	c := &http.Cookie{Name: name, Path: path, MaxAge: -1, Value: ""}
	body := NewOrderedMap()
	body.Set("ok", BoolValue(true))
	return ResponseValue(&Response{ContentType: "application/json", Body: ObjectValue(body), Cookies: []*http.Cookie{c}}), nil
}

// ===== AES-256-GCM =====
//
// aes_encrypt(plaintext, key) returns base64( nonce || ciphertext || tag ).
// The key must be 32 bytes (AES-256). Use hash_sha256(passphrase) for a
// quick way to derive a key from a passphrase, or generate one with uuid()
// twice base64-encoded for full random.

func deriveAESKey(key string) []byte {
	if len(key) == 32 {
		return []byte(key)
	}
	// Derive a 32-byte key with SHA-256 if user passed a passphrase.
	h := sha256.Sum256([]byte(key))
	return h[:]
}

func builtinAESEncrypt(i *Interpreter, args []Value) (Value, error) {
	plaintext, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	block, err := aes.NewCipher(deriveAESKey(key))
	if err != nil {
		return Value{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Value{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return Value{}, err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return StringValue(base64.StdEncoding.EncodeToString(sealed)), nil
}

func builtinAESDecrypt(i *Interpreter, args []Value) (Value, error) {
	ciphertext, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return Value{}, err
	}
	block, err := aes.NewCipher(deriveAESKey(key))
	if err != nil {
		return Value{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Value{}, err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return Value{}, fmt.Errorf("ciphertext too short")
	}
	nonce, body := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return Value{}, err
	}
	return StringValue(string(plaintext)), nil
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

// render(path, vars?, partials?) reads an HTML template from disk and
// renders it. Returns a Response with content-type text/html.
//
//	render("./views/post.html", { post: post })
//	render("./views/page.html", vars, { header: "<h1>Hi</h1>", nav: navHtml })
//
// All `{{ expr }}` substitutions are HTML-escaped. Use `{{{ expr }}}` for
// raw insertion. Supports `{{#if expr}}…{{else}}…{{/if}}`,
// `{{#each items}}…{{/each}}`, and `{{> name}}` partials.
func builtinRender(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return Value{}, err
	}
	vars, partials := templateArgs(args)
	out, err := renderTemplate(string(src), vars, partials, true)
	if err != nil {
		return Value{}, err
	}
	return ResponseValue(&Response{ContentType: "text/html; charset=utf-8", Body: StringValue(out)}), nil
}

// render_string(template, vars?, partials?) renders an inline template
// and returns the resulting string. Caller decides what to do with the
// output (html(), persistence, email body, etc.).
func builtinRenderString(i *Interpreter, args []Value) (Value, error) {
	tmpl, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	vars, partials := templateArgs(args)
	out, err := renderTemplate(tmpl, vars, partials, true)
	if err != nil {
		return Value{}, err
	}
	return StringValue(out), nil
}

// templateArgs pulls `vars` (object) and `partials` (object of
// name->template-string) from the optional positional args.
func templateArgs(args []Value) (*OrderedMap, map[string]string) {
	var vars *OrderedMap
	if len(args) > 1 && args[1].Kind == KindObject {
		vars = args[1].Object
	}
	partials := map[string]string{}
	if len(args) > 2 && args[2].Kind == KindObject {
		for _, k := range args[2].Object.Keys {
			v, _ := args[2].Object.Get(k)
			if v.Kind == KindString {
				partials[k] = v.String
			}
		}
	}
	return vars, partials
}

// ===== Template engine =====
//
// Templates are parsed once into a small AST of nodes (text, interp,
// if, each, partial) and rendered against a stack of scopes so `each`
// can shadow names from outer scopes without losing them.
//
// Tags:
//   {{ expr }}           HTML-escaped interpolation
//   {{{ expr }}}         raw interpolation (no escaping)
//   {{#if expr}}…{{else}}…{{/if}}
//   {{#each items}}…{{/each}}   inside: {{this}}, {{@index}}, {{@key}}
//   {{> name}}           include a partial by name
//
// Expressions are dotted-path lookups against the scope stack: `name`,
// `post.title`, `user.address.city`, `this.name`. There is no expression
// language beyond that — keep templates declarative.

type tmplNode interface{ tmplNode() }

type tmplText struct{ s string }
type tmplInterp struct {
	expr   string
	escape bool
}
type tmplIf struct {
	cond     string
	then     []tmplNode
	elseBody []tmplNode
}
type tmplEach struct {
	expr string
	body []tmplNode
}
type tmplPartial struct{ name string }

func (tmplText) tmplNode()    {}
func (tmplInterp) tmplNode()  {}
func (tmplIf) tmplNode()      {}
func (tmplEach) tmplNode()    {}
func (tmplPartial) tmplNode() {}

// renderTemplate is the public entry point. It parses then renders.
func renderTemplate(tmpl string, vars *OrderedMap, partials map[string]string, escape bool) (string, error) {
	tokens, err := tmplTokenize(tmpl)
	if err != nil {
		return "", err
	}
	nodes, _, err := tmplParse(tokens, 0, "")
	if err != nil {
		return "", err
	}
	scope := []map[string]Value{}
	if vars != nil {
		scope = append(scope, orderedMapToMap(vars))
	}
	var b strings.Builder
	if err := tmplRender(&b, nodes, scope, partials, escape, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// tmplToken is a flat token: text, an interpolation, or one of the
// block-control tags (if/else/endif/each/endeach/partial).
type tmplToken struct {
	kind string // "text", "interp", "raw", "if", "else", "endif", "each", "endeach", "partial"
	body string // text content or expression
}

func tmplTokenize(s string) ([]tmplToken, error) {
	var out []tmplToken
	i := 0
	for i < len(s) {
		// {{{ raw }}}
		if i+2 < len(s) && s[i] == '{' && s[i+1] == '{' && s[i+2] == '{' {
			end := strings.Index(s[i+3:], "}}}")
			if end < 0 {
				return nil, fmt.Errorf("unterminated {{{...}}} in template")
			}
			out = append(out, tmplToken{kind: "raw", body: strings.TrimSpace(s[i+3 : i+3+end])})
			i += 3 + end + 3
			continue
		}
		// {{ ... }}
		if i+1 < len(s) && s[i] == '{' && s[i+1] == '{' {
			end := strings.Index(s[i+2:], "}}")
			if end < 0 {
				return nil, fmt.Errorf("unterminated {{...}} in template")
			}
			body := strings.TrimSpace(s[i+2 : i+2+end])
			i += 2 + end + 2
			switch {
			case strings.HasPrefix(body, "#if "):
				out = append(out, tmplToken{kind: "if", body: strings.TrimSpace(body[3:])})
			case body == "else":
				out = append(out, tmplToken{kind: "else"})
			case body == "/if":
				out = append(out, tmplToken{kind: "endif"})
			case strings.HasPrefix(body, "#each "):
				out = append(out, tmplToken{kind: "each", body: strings.TrimSpace(body[5:])})
			case body == "/each":
				out = append(out, tmplToken{kind: "endeach"})
			case strings.HasPrefix(body, "> "):
				out = append(out, tmplToken{kind: "partial", body: strings.TrimSpace(body[2:])})
			default:
				out = append(out, tmplToken{kind: "interp", body: body})
			}
			continue
		}
		// Run of plain text up to the next `{{`.
		j := i
		for j < len(s) && !(j+1 < len(s) && s[j] == '{' && s[j+1] == '{') {
			j++
		}
		out = append(out, tmplToken{kind: "text", body: s[i:j]})
		i = j
	}
	return out, nil
}

// tmplParse converts a flat token stream into a node tree, stopping
// when it sees `stopAt` (used to find the matching `endif`/`endeach`/
// `else`). Returns the nodes, the index of the stop token, or err.
func tmplParse(tokens []tmplToken, start int, stopAt string) ([]tmplNode, int, error) {
	var nodes []tmplNode
	i := start
	for i < len(tokens) {
		t := tokens[i]
		if stopAt != "" && (t.kind == stopAt || (stopAt == "endif" && t.kind == "else")) {
			return nodes, i, nil
		}
		switch t.kind {
		case "text":
			nodes = append(nodes, tmplText{s: t.body})
			i++
		case "interp":
			nodes = append(nodes, tmplInterp{expr: t.body, escape: true})
			i++
		case "raw":
			nodes = append(nodes, tmplInterp{expr: t.body, escape: false})
			i++
		case "partial":
			nodes = append(nodes, tmplPartial{name: t.body})
			i++
		case "if":
			cond := t.body
			thenNodes, next, err := tmplParse(tokens, i+1, "endif")
			if err != nil {
				return nil, 0, err
			}
			if next >= len(tokens) {
				return nil, 0, fmt.Errorf("unterminated {{#if %s}}", cond)
			}
			var elseNodes []tmplNode
			if tokens[next].kind == "else" {
				elseNodes, next, err = tmplParse(tokens, next+1, "endif")
				if err != nil {
					return nil, 0, err
				}
				if next >= len(tokens) || tokens[next].kind != "endif" {
					return nil, 0, fmt.Errorf("unterminated {{#if %s}}", cond)
				}
			}
			nodes = append(nodes, tmplIf{cond: cond, then: thenNodes, elseBody: elseNodes})
			i = next + 1
		case "each":
			expr := t.body
			body, next, err := tmplParse(tokens, i+1, "endeach")
			if err != nil {
				return nil, 0, err
			}
			if next >= len(tokens) || tokens[next].kind != "endeach" {
				return nil, 0, fmt.Errorf("unterminated {{#each %s}}", expr)
			}
			nodes = append(nodes, tmplEach{expr: expr, body: body})
			i = next + 1
		case "else", "endif", "endeach":
			return nil, 0, fmt.Errorf("stray {{%s}} with no matching opener", t.kind)
		default:
			return nil, 0, fmt.Errorf("unknown template token %q", t.kind)
		}
	}
	return nodes, i, nil
}

// tmplRender walks the node tree against the scope stack and writes
// output to b. depth guards against pathological partial recursion.
func tmplRender(b *strings.Builder, nodes []tmplNode, scope []map[string]Value, partials map[string]string, escape bool, depth int) error {
	if depth > 16 {
		return fmt.Errorf("template partial recursion exceeded depth 16")
	}
	for _, n := range nodes {
		switch v := n.(type) {
		case tmplText:
			b.WriteString(v.s)
		case tmplInterp:
			val := lookupTemplateExpr(scope, v.expr)
			s := val.Display()
			if v.escape {
				s = htmlEscapeString(s)
			}
			b.WriteString(s)
		case tmplPartial:
			tmpl, ok := partials[v.name]
			if !ok {
				return fmt.Errorf("template partial %q not provided", v.name)
			}
			tokens, err := tmplTokenize(tmpl)
			if err != nil {
				return err
			}
			child, _, err := tmplParse(tokens, 0, "")
			if err != nil {
				return err
			}
			if err := tmplRender(b, child, scope, partials, escape, depth+1); err != nil {
				return err
			}
		case tmplIf:
			val := lookupTemplateExpr(scope, v.cond)
			if isTemplateTruthy(val) {
				if err := tmplRender(b, v.then, scope, partials, escape, depth); err != nil {
					return err
				}
			} else if len(v.elseBody) > 0 {
				if err := tmplRender(b, v.elseBody, scope, partials, escape, depth); err != nil {
					return err
				}
			}
		case tmplEach:
			val := lookupTemplateExpr(scope, v.expr)
			switch val.Kind {
			case KindArray:
				for idx, item := range val.Array {
					frame := map[string]Value{
						"this":   item,
						"@index": NumberValue(float64(idx)),
					}
					// Object items expose their keys directly so templates
					// can write `{{title}}` instead of `{{this.title}}`.
					if item.Kind == KindObject {
						for _, k := range item.Object.Keys {
							iv, _ := item.Object.Get(k)
							frame[k] = iv
						}
					}
					if err := tmplRender(b, v.body, append(scope, frame), partials, escape, depth); err != nil {
						return err
					}
				}
			case KindObject:
				for idx, k := range val.Object.Keys {
					iv, _ := val.Object.Get(k)
					frame := map[string]Value{
						"this":   iv,
						"@key":   StringValue(k),
						"@index": NumberValue(float64(idx)),
					}
					if iv.Kind == KindObject {
						for _, kk := range iv.Object.Keys {
							ivv, _ := iv.Object.Get(kk)
							frame[kk] = ivv
						}
					}
					if err := tmplRender(b, v.body, append(scope, frame), partials, escape, depth); err != nil {
						return err
					}
				}
			default:
				// Null / non-iterable: render zero times — same semantics
				// as Handlebars / Mustache. Don't error.
			}
		}
	}
	return nil
}

// lookupTemplateExpr resolves a dotted path against the scope stack.
// Inner scopes shadow outer; identifiers starting with `@` and `this`
// always come from the innermost frame.
func lookupTemplateExpr(scope []map[string]Value, expr string) Value {
	if expr == "" {
		return NullValue()
	}
	parts := strings.Split(expr, ".")
	for k := len(scope) - 1; k >= 0; k-- {
		if v, ok := scope[k][parts[0]]; ok {
			return walkValuePath(v, parts[1:])
		}
	}
	return NullValue()
}

func walkValuePath(v Value, parts []string) Value {
	for _, p := range parts {
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

// isTemplateTruthy uses the same rules as MX's IsTruthy plus a special
// case for empty arrays / empty strings / null so templates feel like
// the user expects.
func isTemplateTruthy(v Value) bool {
	switch v.Kind {
	case KindNull:
		return false
	case KindBool:
		return v.Bool
	case KindNumber:
		return v.Number != 0
	case KindString:
		return v.String != ""
	case KindArray:
		return len(v.Array) > 0
	case KindObject:
		return len(v.Object.Keys) > 0
	}
	return true
}

func orderedMapToMap(o *OrderedMap) map[string]Value {
	out := make(map[string]Value, len(o.Keys))
	for _, k := range o.Keys {
		v, _ := o.Get(k)
		out[k] = v
	}
	return out
}

func htmlEscapeString(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// ===== SQL builtins =====
//
// Implementations live in sql.go (with a `!js` build tag) and a stub
// set in sql_wasm.go so the wasm build links cleanly.

// ===== Image =====
//
// Image bytes flow through MX as strings (the same shape multipart file
// content uses, so request.files?.image.content goes straight to these
// functions). Helpers:
//
//   image.info(bytes)              -> { width, height, format }
//   image.resize(bytes, w, h, opts?) -> resized bytes
//   image.convert(bytes, format)   -> bytes (jpeg / png)
//
// Pure stdlib (image, image/png, image/jpeg, image/gif). No GIF write,
// no WebP, no AVIF — open a PR if you need them.

func builtinImageInfo(i *Interpreter, args []Value) (Value, error) {
	data, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	cfg, format, err := image.DecodeConfig(strings.NewReader(data))
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	out.Set("format", StringValue(format))
	out.Set("width", NumberValue(float64(cfg.Width)))
	out.Set("height", NumberValue(float64(cfg.Height)))
	return ObjectValue(out), nil
}

func builtinImageResize(i *Interpreter, args []Value) (Value, error) {
	data, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	w, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	h, err := numberArg(args, 2)
	if err != nil {
		return Value{}, err
	}
	src, format, err := image.Decode(strings.NewReader(data))
	if err != nil {
		return Value{}, err
	}
	// Override format if opts.format is set.
	outFormat := format
	quality := 85
	if len(args) > 3 && args[3].Kind == KindObject {
		if v, ok := args[3].Object.Get("format"); ok && v.Kind == KindString {
			outFormat = v.String
		}
		if v, ok := args[3].Object.Get("quality"); ok && v.Kind == KindNumber {
			quality = int(v.Number)
		}
	}

	dst := imageNearestNeighborResize(src, int(w), int(h))
	return encodeImage(dst, outFormat, quality)
}

// image.thumbnail(bytes, max_size, opts?) — fits the image within a
// `max_size × max_size` square, preserving aspect ratio. Useful for
// avatars and previews where you want a single dimension cap rather
// than fixed width × height.
func builtinImageThumbnail(_ *Interpreter, args []Value) (Value, error) {
	data, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	maxSize, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	src, format, err := image.Decode(strings.NewReader(data))
	if err != nil {
		return Value{}, err
	}
	outFormat := format
	quality := 85
	if len(args) > 2 && args[2].Kind == KindObject {
		if v, ok := args[2].Object.Get("format"); ok && v.Kind == KindString {
			outFormat = v.String
		}
		if v, ok := args[2].Object.Get("quality"); ok && v.Kind == KindNumber {
			quality = int(v.Number)
		}
	}

	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	max := int(maxSize)
	if srcW <= max && srcH <= max {
		// Already small enough; just re-encode in case format changed.
		return encodeImage(src, outFormat, quality)
	}
	var dstW, dstH int
	if srcW > srcH {
		dstW = max
		dstH = srcH * max / srcW
	} else {
		dstH = max
		dstW = srcW * max / srcH
	}
	dst := imageNearestNeighborResize(src, dstW, dstH)
	return encodeImage(dst, outFormat, quality)
}

// image.crop(bytes, x, y, w, h, opts?) — extract a rectangular region.
// Coordinates are top-left origin, clamped to the source bounds.
func builtinImageCrop(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 5 {
		return Value{}, fmt.Errorf("image.crop(bytes, x, y, w, h, opts?) requires 5 args")
	}
	data, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	x, err := numberArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	y, err := numberArg(args, 2)
	if err != nil {
		return Value{}, err
	}
	w, err := numberArg(args, 3)
	if err != nil {
		return Value{}, err
	}
	h, err := numberArg(args, 4)
	if err != nil {
		return Value{}, err
	}
	src, format, err := image.Decode(strings.NewReader(data))
	if err != nil {
		return Value{}, err
	}
	outFormat := format
	quality := 85
	if len(args) > 5 && args[5].Kind == KindObject {
		if v, ok := args[5].Object.Get("format"); ok && v.Kind == KindString {
			outFormat = v.String
		}
		if v, ok := args[5].Object.Get("quality"); ok && v.Kind == KindNumber {
			quality = int(v.Number)
		}
	}

	srcB := src.Bounds()
	rect := image.Rect(srcB.Min.X+int(x), srcB.Min.Y+int(y),
		srcB.Min.X+int(x+w), srcB.Min.Y+int(y+h))
	rect = rect.Intersect(srcB)
	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for cy := 0; cy < rect.Dy(); cy++ {
		for cx := 0; cx < rect.Dx(); cx++ {
			dst.Set(cx, cy, src.At(rect.Min.X+cx, rect.Min.Y+cy))
		}
	}
	return encodeImage(dst, outFormat, quality)
}

func builtinImageConvert(i *Interpreter, args []Value) (Value, error) {
	data, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	format, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	src, _, err := image.Decode(strings.NewReader(data))
	if err != nil {
		return Value{}, err
	}
	quality := 85
	if len(args) > 2 && args[2].Kind == KindNumber {
		quality = int(args[2].Number)
	}
	return encodeImage(src, format, quality)
}

// imageNearestNeighborResize is fast and good enough for thumbnails.
// For higher-quality (Lanczos/bilinear) resizing the user can pull in
// golang.org/x/image — we keep stdlib-only here.
func imageNearestNeighborResize(src image.Image, dstW, dstH int) image.Image {
	srcB := src.Bounds()
	srcW := srcB.Dx()
	srcH := srcB.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		sy := y * srcH / dstH
		for x := 0; x < dstW; x++ {
			sx := x * srcW / dstW
			dst.Set(x, y, src.At(srcB.Min.X+sx, srcB.Min.Y+sy))
		}
	}
	return dst
}

func encodeImage(img image.Image, format string, quality int) (Value, error) {
	var buf bytes.Buffer
	switch strings.ToLower(format) {
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return Value{}, err
		}
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return Value{}, err
		}
	default:
		return Value{}, fmt.Errorf("unsupported image format %q (use png or jpeg)", format)
	}
	return StringValue(buf.String()), nil
}

// ===== OAuth2 =====
//
// Generic helpers for any OAuth2 provider — Google, GitHub, Discord,
// Auth0, etc. Two functions: build the user-facing authorize URL, then
// exchange the callback ?code= for an access token.
//
//	let url = oauth.authorize_url({
//	  provider: "google",  // or "github", or pass authorize_url explicitly
//	  client_id: env_required("GOOGLE_CLIENT_ID"),
//	  redirect_uri: "https://app.example.com/auth/callback",
//	  scopes: ["openid", "email", "profile"]
//	})
//	// redirect user to `url`. After they consent, Google redirects back
//	// with ?code=... Then:
//	let tokens = oauth.exchange_code({
//	  provider: "google",
//	  client_id: env_required("GOOGLE_CLIENT_ID"),
//	  client_secret: env_required("GOOGLE_CLIENT_SECRET"),
//	  redirect_uri: "https://app.example.com/auth/callback",
//	  code: request.query.code
//	})

var oauthProviders = map[string]struct{ Auth, Token string }{
	"google":    {"https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token"},
	"github":    {"https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token"},
	"discord":   {"https://discord.com/api/oauth2/authorize", "https://discord.com/api/oauth2/token"},
	"linkedin":  {"https://www.linkedin.com/oauth/v2/authorization", "https://www.linkedin.com/oauth/v2/accessToken"},
	"microsoft": {"https://login.microsoftonline.com/common/oauth2/v2.0/authorize", "https://login.microsoftonline.com/common/oauth2/v2.0/token"},
}

func resolveOAuthEndpoint(opts *OrderedMap, kind string) string {
	if v, ok := opts.Get(kind + "_url"); ok && v.Kind == KindString {
		return v.String
	}
	if v, ok := opts.Get("provider"); ok && v.Kind == KindString {
		if p, ok := oauthProviders[strings.ToLower(v.String)]; ok {
			if kind == "authorize" {
				return p.Auth
			}
			return p.Token
		}
	}
	return ""
}

func builtinOAuthAuthorizeURL(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("oauth.authorize_url(opts) requires an object")
	}
	o := args[0].Object
	authorize := resolveOAuthEndpoint(o, "authorize")
	if authorize == "" {
		return Value{}, fmt.Errorf("oauth.authorize_url: pass `provider` (google/github/discord/...) or `authorize_url` explicitly")
	}
	clientID := ""
	if v, ok := o.Get("client_id"); ok && v.Kind == KindString {
		clientID = v.String
	}
	if clientID == "" {
		return Value{}, fmt.Errorf("oauth.authorize_url: client_id is required")
	}

	q := neturl.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	if v, ok := o.Get("redirect_uri"); ok && v.Kind == KindString {
		q.Set("redirect_uri", v.String)
	}
	if v, ok := o.Get("state"); ok && v.Kind == KindString {
		q.Set("state", v.String)
	}
	if v, ok := o.Get("scopes"); ok && v.Kind == KindArray {
		q.Set("scope", strings.Join(stringsFromArray(v.Array), " "))
	} else if v, ok := o.Get("scope"); ok && v.Kind == KindString {
		q.Set("scope", v.String)
	}
	// Provider-specific extras the user can pass through.
	for _, k := range []string{"prompt", "access_type", "response_mode"} {
		if v, ok := o.Get(k); ok && v.Kind == KindString {
			q.Set(k, v.String)
		}
	}
	return StringValue(authorize + "?" + q.Encode()), nil
}

func builtinOAuthExchangeCode(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("oauth.exchange_code(opts) requires an object")
	}
	o := args[0].Object
	tokenURL := resolveOAuthEndpoint(o, "token")
	if tokenURL == "" {
		return Value{}, fmt.Errorf("oauth.exchange_code: pass `provider` or `token_url`")
	}

	form := neturl.Values{}
	form.Set("grant_type", "authorization_code")
	for _, k := range []string{"client_id", "client_secret", "redirect_uri", "code"} {
		if v, ok := o.Get(k); ok && v.Kind == KindString {
			form.Set(k, v.String)
		}
	}
	if form.Get("client_id") == "" || form.Get("code") == "" {
		return Value{}, fmt.Errorf("oauth.exchange_code requires client_id and code")
	}

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
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
		return Value{}, fmt.Errorf("oauth.exchange_code failed (%d): %s", resp.StatusCode, string(raw))
	}
	// Response is usually JSON, sometimes form-encoded (looking at you,
	// older GitHub). Try JSON first, fall back to form.
	if v, err := jsonDecode(raw); err == nil && v.Kind == KindObject {
		return v, nil
	}
	if vals, err := neturl.ParseQuery(string(raw)); err == nil {
		out := NewOrderedMap()
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out.Set(k, StringValue(vals.Get(k)))
		}
		return ObjectValue(out), nil
	}
	return Value{}, fmt.Errorf("oauth.exchange_code: cannot parse response: %s", string(raw))
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

func computeHMACBase64(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ===== Webhook namespace =====
//
// Each verify_* takes a payload (the raw body string), the relevant
// header values, and the shared secret, and returns a boolean. They
// follow each provider's documented scheme exactly so tests built from
// the published examples pass without modification.

// webhooks.verify_stripe(payload, signature_header, secret, tolerance_seconds?) -> bool
//
// Verifies a Stripe-Signature header in the form
// "t=<timestamp>,v1=<hex>[,v1=<hex>...]". Returns true only if at least
// one v1 entry matches HMAC-SHA256(secret, "<timestamp>.<payload>") AND
// the timestamp is within `tolerance_seconds` of the current time.
// Default tolerance is 300 seconds (5 minutes), matching Stripe's docs.
//
//	let ok = webhooks.verify_stripe(
//	  request.body_text,
//	  request.headers["stripe-signature"],
//	  env("STRIPE_WEBHOOK_SECRET")
//	)
func builtinWebhooksVerifyStripe(i *Interpreter, args []Value) (Value, error) {
	payload, sigHeader, secret, err := webhookThreeStringArgs(args, "webhooks.verify_stripe")
	if err != nil {
		return Value{}, err
	}
	tolerance := int64(300)
	if len(args) > 3 && args[3].Kind == KindNumber {
		tolerance = int64(args[3].Number)
	}
	var ts string
	var v1s []string
	for _, part := range strings.Split(sigHeader, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			v1s = append(v1s, kv[1])
		}
	}
	if ts == "" || len(v1s) == 0 {
		return BoolValue(false), nil
	}
	tsNum, parseErr := strconv.ParseInt(ts, 10, 64)
	if parseErr != nil {
		return BoolValue(false), nil
	}
	if tolerance > 0 {
		now := time.Now().Unix()
		drift := now - tsNum
		if drift < 0 {
			drift = -drift
		}
		if drift > tolerance {
			return BoolValue(false), nil
		}
	}
	expected := computeHMACHex(secret, ts+"."+payload)
	for _, v := range v1s {
		if hmac.Equal([]byte(expected), []byte(v)) {
			return BoolValue(true), nil
		}
	}
	return BoolValue(false), nil
}

// webhooks.verify_github(payload, signature_header, secret) -> bool
//
// GitHub signs with X-Hub-Signature-256 = "sha256=<hex>".
func builtinWebhooksVerifyGitHub(i *Interpreter, args []Value) (Value, error) {
	payload, sig, secret, err := webhookThreeStringArgs(args, "webhooks.verify_github")
	if err != nil {
		return Value{}, err
	}
	const prefix = "sha256="
	if !strings.HasPrefix(sig, prefix) {
		return BoolValue(false), nil
	}
	expected := computeHMACHex(secret, payload)
	return BoolValue(hmac.Equal([]byte(expected), []byte(sig[len(prefix):]))), nil
}

// webhooks.verify_svix(payload, msg_id, timestamp, signature_header, secret) -> bool
//
// Svix (Resend, Clerk, Discord, etc.) signs with HMAC-SHA256 of
// "<msg_id>.<timestamp>.<payload>". The signature header is a
// space-separated list of "v1,<base64>" entries. The secret comes from
// the dashboard prefixed with "whsec_" — we strip and base64-decode it
// per the Svix spec. Returns true if any signature matches.
//
//	let ok = webhooks.verify_svix(
//	  request.body_text,
//	  request.headers["svix-id"],
//	  request.headers["svix-timestamp"],
//	  request.headers["svix-signature"],
//	  env("SVIX_SECRET")
//	)
func builtinWebhooksVerifySvix(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 5 {
		return Value{}, fmt.Errorf("webhooks.verify_svix(payload, msg_id, timestamp, signature, secret)")
	}
	payload, _ := stringArg(args, 0)
	msgID, _ := stringArg(args, 1)
	timestamp, _ := stringArg(args, 2)
	sigHeader, _ := stringArg(args, 3)
	secret, _ := stringArg(args, 4)

	if msgID == "" || timestamp == "" || sigHeader == "" || secret == "" {
		return BoolValue(false), nil
	}
	// Strip the "whsec_" prefix and base64-decode to get the raw key.
	keyB64 := strings.TrimPrefix(secret, "whsec_")
	keyBytes, decErr := base64.StdEncoding.DecodeString(keyB64)
	if decErr != nil {
		// If the user passes the raw key directly (not base64), use it as-is.
		keyBytes = []byte(secret)
	}
	signedString := msgID + "." + timestamp + "." + payload
	mac := hmac.New(sha256.New, keyBytes)
	mac.Write([]byte(signedString))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for _, entry := range strings.Fields(sigHeader) {
		// Each entry is "v1,<base64>"
		kv := strings.SplitN(entry, ",", 2)
		if len(kv) != 2 || kv[0] != "v1" {
			continue
		}
		if hmac.Equal([]byte(expected), []byte(kv[1])) {
			return BoolValue(true), nil
		}
	}
	return BoolValue(false), nil
}

// webhooks.verify_shopify(payload, signature, secret) -> bool
//
// Shopify signs with X-Shopify-Hmac-Sha256 = base64(HMAC-SHA256(body)).
func builtinWebhooksVerifyShopify(i *Interpreter, args []Value) (Value, error) {
	payload, sig, secret, err := webhookThreeStringArgs(args, "webhooks.verify_shopify")
	if err != nil {
		return Value{}, err
	}
	expected := computeHMACBase64(secret, payload)
	return BoolValue(hmac.Equal([]byte(expected), []byte(sig))), nil
}

// webhooks.verify_slack(payload, timestamp, signature, secret, tolerance_seconds?) -> bool
//
// Slack signs with X-Slack-Signature = "v0=<hex>" where the signed
// string is "v0:<timestamp>:<payload>". The X-Slack-Request-Timestamp
// header guards against replay; default tolerance 300s.
func builtinWebhooksVerifySlack(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 4 {
		return Value{}, fmt.Errorf("webhooks.verify_slack(payload, timestamp, signature, secret, tolerance?)")
	}
	payload, _ := stringArg(args, 0)
	timestamp, _ := stringArg(args, 1)
	sig, _ := stringArg(args, 2)
	secret, _ := stringArg(args, 3)
	tolerance := int64(300)
	if len(args) > 4 && args[4].Kind == KindNumber {
		tolerance = int64(args[4].Number)
	}
	const prefix = "v0="
	if !strings.HasPrefix(sig, prefix) {
		return BoolValue(false), nil
	}
	tsNum, parseErr := strconv.ParseInt(timestamp, 10, 64)
	if parseErr != nil {
		return BoolValue(false), nil
	}
	if tolerance > 0 {
		now := time.Now().Unix()
		drift := now - tsNum
		if drift < 0 {
			drift = -drift
		}
		if drift > tolerance {
			return BoolValue(false), nil
		}
	}
	expected := "v0=" + computeHMACHex(secret, "v0:"+timestamp+":"+payload)
	return BoolValue(hmac.Equal([]byte(expected), []byte(sig))), nil
}

// webhookThreeStringArgs is a tiny shared validator: every verify_*
// helper takes (payload, signature, secret) so we centralise the type
// check + error message.
func webhookThreeStringArgs(args []Value, name string) (string, string, string, error) {
	if len(args) < 3 {
		return "", "", "", fmt.Errorf("%s(payload, signature, secret)", name)
	}
	for k, a := range args[:3] {
		if a.Kind != KindString {
			return "", "", "", fmt.Errorf("%s: argument %d must be a string", name, k+1)
		}
	}
	return args[0].String, args[1].String, args[2].String, nil
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

// time_ago(unix_ms) → "5 minutes ago", "in 2 days", "just now", etc.
func builtinTimeAgo(i *Interpreter, args []Value) (Value, error) {
	ms, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	d := time.Since(time.UnixMilli(int64(ms)))
	future := false
	if d < 0 {
		future = true
		d = -d
	}
	var s string
	switch {
	case d < 30*time.Second:
		return StringValue("just now"), nil
	case d < time.Minute:
		s = fmt.Sprintf("%d seconds", int(d.Seconds()))
	case d < time.Hour:
		s = pluralUnit(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		s = pluralUnit(int(d.Hours()), "hour")
	case d < 7*24*time.Hour:
		s = pluralUnit(int(d.Hours()/24), "day")
	case d < 30*24*time.Hour:
		s = pluralUnit(int(d.Hours()/(24*7)), "week")
	case d < 365*24*time.Hour:
		s = pluralUnit(int(d.Hours()/(24*30)), "month")
	default:
		s = pluralUnit(int(d.Hours()/(24*365)), "year")
	}
	if future {
		return StringValue("in " + s), nil
	}
	return StringValue(s + " ago"), nil
}

// time_human(unix_ms) → "Sat May 2 2026 14:23"
func builtinTimeHuman(i *Interpreter, args []Value) (Value, error) {
	ms, err := numberArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	return StringValue(msToTime(ms).Local().Format("Mon Jan 2 2006 15:04")), nil
}

func pluralUnit(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
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

// cron(spec, fn) runs fn() on a Vixie-cron schedule. Spec is the
// standard 5-field expression: "minute hour day-of-month month day-of-week".
// Returns a stop function that cancels the schedule.
//
//	let stop = cron("0 9 * * 1-5", fn() { send_daily_digest() })  // 09:00 weekdays
//	cron("*/5 * * * *", fn() { sweep_jobs() })                    // every 5 minutes
//	cron("0 0 1 * *",  fn() { roll_invoices() })                  // 1st of each month
//
// Errors in fn are logged but do not stop the schedule. The goroutine
// exits cleanly when stop() is called.
func builtinCron(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("cron(spec, fn) requires (string, function)")
	}
	schedule, err := ParseCron(args[0].String)
	if err != nil {
		return Value{}, err
	}
	fn := args[1].Function
	stop := make(chan struct{})

	go func() {
		for {
			next := schedule.Next(time.Now())
			if next.IsZero() {
				fmt.Fprintf(os.Stderr, "[mx cron] schedule %q never fires within 4 years\n", args[0].String)
				return
			}
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}
			select {
			case <-stop:
				return
			case <-time.After(delay):
				if _, err := i.callFunction(nil, fn, nil); err != nil {
					fmt.Fprintf(os.Stderr, "[mx cron %q] %v\n", args[0].String, err)
					// Don't return — let the next firing have a chance.
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

// ===== CSRF =====
//
// csrf_token(secret, session_id) returns a deterministic token for a
// given (secret, session_id) pair. Embed it in your forms / JS state,
// then verify on every state-changing request:
//
//	get / {
//	  let sid = request.cookies?.sid ?? "anon"
//	  return html("<form method=POST><input name=_csrf value='" + csrf_token(env("CSRF_SECRET"), sid) + "'>...</form>")
//	}
//
//	post /transfer {
//	  let sid = request.cookies?.sid ?? "anon"
//	  if (!verify_csrf(env("CSRF_SECRET"), sid, request.body._csrf)) {
//	    return status(403, { error: "csrf token mismatch" })
//	  }
//	  ...
//	}

func builtinCSRFToken(i *Interpreter, args []Value) (Value, error) {
	secret, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	sessionID := ""
	if len(args) > 1 && args[1].Kind == KindString {
		sessionID = args[1].String
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(sessionID))
	return StringValue(base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), nil
}

func builtinVerifyCSRF(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 3 {
		return Value{}, fmt.Errorf("verify_csrf(secret, session_id, token) requires 3 args")
	}
	secret, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	sessionID, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	token, err := stringArg(args, 2)
	if err != nil {
		return Value{}, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(sessionID))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return BoolValue(hmac.Equal([]byte(expected), []byte(token))), nil
}

// ===== Pub/sub =====
//
// pubsub.topic(name?) returns an object with .subscribe(fn) and
// .publish(value). Subscriptions are in-process, fan-out to every live
// subscriber. Use it to broadcast WebSocket messages across all
// connected clients without a per-route registry:
//
//	let chat = pubsub.topic("chat")
//
//	ws /chat {
//	  let sub = chat.subscribe(send)
//	  while (true) {
//	    let m = recv()
//	    if (m == null) { break }
//	    chat.publish(m)
//	  }
//	  sub.unsubscribe()
//	}

type pubsubTopic struct {
	mu    sync.Mutex
	subs  map[int64]*Function
	nextI int64
}

func newPubsubTopic() *pubsubTopic { return &pubsubTopic{subs: map[int64]*Function{}} }

func builtinPubsubTopic(i *Interpreter, args []Value) (Value, error) {
	t := newPubsubTopic()
	out := NewOrderedMap()
	out.Set("subscribe", FunctionValue(&Function{Name: "topic.subscribe", Native: func(_ *Interpreter, a []Value) (Value, error) {
		if len(a) < 1 || a[0].Kind != KindFunction {
			return Value{}, fmt.Errorf("subscribe(fn) requires a function")
		}
		t.mu.Lock()
		t.nextI++
		id := t.nextI
		t.subs[id] = a[0].Function
		t.mu.Unlock()
		// Return an object with .unsubscribe()
		sub := NewOrderedMap()
		sub.Set("unsubscribe", FunctionValue(&Function{Name: "subscription.unsubscribe", Native: func(_ *Interpreter, _ []Value) (Value, error) {
			t.mu.Lock()
			delete(t.subs, id)
			t.mu.Unlock()
			return NullValue(), nil
		}}))
		return ObjectValue(sub), nil
	}}))
	out.Set("publish", FunctionValue(&Function{Name: "topic.publish", Native: func(interp *Interpreter, a []Value) (Value, error) {
		if len(a) < 1 {
			return Value{}, fmt.Errorf("publish(value) requires a value")
		}
		// Snapshot subscribers so callbacks can unsubscribe without
		// mutating the map mid-iteration.
		t.mu.Lock()
		subs := make([]*Function, 0, len(t.subs))
		for _, f := range t.subs {
			subs = append(subs, f)
		}
		t.mu.Unlock()
		for _, f := range subs {
			if _, err := interp.callFunction(nil, f, []Value{a[0]}); err != nil {
				// Swallow errors — a bad subscriber shouldn't take down the topic.
				fmt.Fprintf(os.Stderr, "[mx pubsub] subscriber error: %v\n", err)
			}
		}
		return NumberValue(float64(len(subs))), nil
	}}))
	out.Set("count", FunctionValue(&Function{Name: "topic.count", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		t.mu.Lock()
		n := len(t.subs)
		t.mu.Unlock()
		return NumberValue(float64(n)), nil
	}}))
	return ObjectValue(out), nil
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

// validate(value, schema) checks `value` against a JSON-Schema-lite spec
// and returns { valid: bool, errors: [{ path, message }, ...] }.
//
//	let schema = {
//	  type: "object",
//	  properties: {
//	    name: { type: "string", min_length: 2 },
//	    age:  { type: "number", minimum: 0, maximum: 150 },
//	    email: { type: "string", format: "email" }
//	  },
//	  required: ["name", "email"]
//	}
//	let r = validate(request.body, schema)
//	if (!r.valid) { return status(400, { errors: r.errors }) }
//
// Supported schema keys: type, required, properties, items, min/max,
// min_length / max_length, pattern, format ("email"|"url"|"uuid"|"date"),
// enum.
func builtinValidate(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("validate(value, schema) requires 2 arguments")
	}
	if args[1].Kind != KindObject {
		return Value{}, fmt.Errorf("validate: schema must be an object")
	}
	var errs []Value
	validateValue(args[0], args[1], "$", &errs)
	out := NewOrderedMap()
	out.Set("valid", BoolValue(len(errs) == 0))
	out.Set("errors", ArrayValue(errs))
	return ObjectValue(out), nil
}

func validateValue(v, schema Value, path string, errs *[]Value) {
	if schema.Kind != KindObject {
		return
	}
	s := schema.Object
	pushErr := func(msg string) {
		e := NewOrderedMap()
		e.Set("path", StringValue(path))
		e.Set("message", StringValue(msg))
		*errs = append(*errs, ObjectValue(e))
	}

	// type
	if t, ok := s.Get("type"); ok && t.Kind == KindString {
		want := t.String
		got := v.typeName()
		// Accept "integer" as numeric int.
		if want == "integer" {
			if v.Kind != KindNumber || v.Number != float64(int64(v.Number)) {
				pushErr(fmt.Sprintf("expected integer, got %s", got))
				return
			}
		} else if want != got {
			// "any" matches anything; null is its own type.
			if want != "any" {
				pushErr(fmt.Sprintf("expected %s, got %s", want, got))
				return
			}
		}
	}

	// enum
	if en, ok := s.Get("enum"); ok && en.Kind == KindArray {
		matched := false
		for _, e := range en.Array {
			if valuesEqual(v, e) {
				matched = true
				break
			}
		}
		if !matched {
			pushErr("value not in enum")
		}
	}

	switch v.Kind {
	case KindNumber:
		if min, ok := s.Get("minimum"); ok && min.Kind == KindNumber && v.Number < min.Number {
			pushErr(fmt.Sprintf("must be >= %g", min.Number))
		}
		if max, ok := s.Get("maximum"); ok && max.Kind == KindNumber && v.Number > max.Number {
			pushErr(fmt.Sprintf("must be <= %g", max.Number))
		}
	case KindString:
		if mn, ok := s.Get("min_length"); ok && mn.Kind == KindNumber && len([]rune(v.String)) < int(mn.Number) {
			pushErr(fmt.Sprintf("must be at least %d chars", int(mn.Number)))
		}
		if mx, ok := s.Get("max_length"); ok && mx.Kind == KindNumber && len([]rune(v.String)) > int(mx.Number) {
			pushErr(fmt.Sprintf("must be at most %d chars", int(mx.Number)))
		}
		if pat, ok := s.Get("pattern"); ok && pat.Kind == KindString {
			if re, err := regexp.Compile(pat.String); err == nil && !re.MatchString(v.String) {
				pushErr("does not match pattern")
			}
		}
		if f, ok := s.Get("format"); ok && f.Kind == KindString {
			if !validateFormat(v.String, f.String) {
				pushErr("invalid " + f.String + " format")
			}
		}
	case KindArray:
		if items, ok := s.Get("items"); ok && items.Kind == KindObject {
			for k, el := range v.Array {
				validateValue(el, items, fmt.Sprintf("%s[%d]", path, k), errs)
			}
		}
		if mn, ok := s.Get("min_items"); ok && mn.Kind == KindNumber && len(v.Array) < int(mn.Number) {
			pushErr(fmt.Sprintf("must have at least %d items", int(mn.Number)))
		}
		if mx, ok := s.Get("max_items"); ok && mx.Kind == KindNumber && len(v.Array) > int(mx.Number) {
			pushErr(fmt.Sprintf("must have at most %d items", int(mx.Number)))
		}
	case KindObject:
		if req, ok := s.Get("required"); ok && req.Kind == KindArray {
			for _, r := range req.Array {
				if r.Kind != KindString {
					continue
				}
				if _, present := v.Object.Get(r.String); !present {
					pushErr(fmt.Sprintf("required property %q is missing", r.String))
				}
			}
		}
		if props, ok := s.Get("properties"); ok && props.Kind == KindObject {
			for _, propName := range props.Object.Keys {
				propSchema, _ := props.Object.Get(propName)
				if propVal, present := v.Object.Get(propName); present {
					validateValue(propVal, propSchema, path+"."+propName, errs)
				}
			}
		}
	}
}

var emailRE = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func validateFormat(s, format string) bool {
	switch format {
	case "email":
		return emailRE.MatchString(s)
	case "uuid":
		return uuidRE.MatchString(s)
	case "url":
		_, err := neturl.ParseRequestURI(s)
		return err == nil
	case "date":
		return dateRE.MatchString(s)
	}
	return true // unknown formats pass; users can use `pattern` for stricter checks
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

	provider := "openai"
	model := "gpt-4o-mini"
	maxTokens := 256
	var tools []Value
	var messages []Value

	if len(args) > 1 && args[1].Kind == KindObject {
		opts := args[1].Object
		if v, ok := opts.Get("provider"); ok && v.Kind == KindString {
			provider = strings.ToLower(v.String)
		}
		if v, ok := opts.Get("model"); ok && v.Kind == KindString {
			model = v.String
		}
		if v, ok := opts.Get("max_tokens"); ok && v.Kind == KindNumber {
			maxTokens = int(v.Number)
		}
		if v, ok := opts.Get("tools"); ok && v.Kind == KindArray {
			tools = v.Array
		}
		// Multi-turn: `messages` overrides `prompt` if both are passed.
		if v, ok := opts.Get("messages"); ok && v.Kind == KindArray {
			messages = v.Array
		}
	}

	if provider == "anthropic" {
		return aiCompleteAnthropic(prompt, model, maxTokens)
	}
	if provider == "gemini" || provider == "google" {
		return aiCompleteGemini(prompt, model, maxTokens)
	}
	if cfg, ok := openAICompatProviders[provider]; ok {
		return aiCompleteOpenAICompat(cfg, prompt, model, maxTokens)
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.complete requires OPENAI_API_KEY environment variable")
	}

	// Tool calling path: structured request + response.
	if len(tools) > 0 || len(messages) > 0 {
		return aiCompleteOpenAITools(prompt, messages, model, maxTokens, tools, apiKey)
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

// aiCompleteOpenAITools is the tool-calling / multi-turn variant.
// Returns either a string (assistant text) or an object with shape
//
//	{ tool_calls: [ { id, name, arguments }, ... ] }
//
// when the model wants to invoke a tool. Callers run the loop:
//
//	let r = ai.complete(prompt, { tools: [...] })
//	if (r.tool_calls != null) {
//	  // execute each tool, build messages, call again
//	} else {
//	  print(r)   // final string answer
//	}
func aiCompleteOpenAITools(prompt string, mxMessages []Value, model string, maxTokens int, tools []Value, apiKey string) (Value, error) {
	// Build messages array. If user passed messages explicitly, use those;
	// otherwise wrap prompt as a single user turn.
	var msgs []map[string]any
	if len(mxMessages) > 0 {
		for _, m := range mxMessages {
			if m.Kind != KindObject {
				continue
			}
			conv := map[string]any{}
			for _, k := range m.Object.Keys {
				v, _ := m.Object.Get(k)
				switch v.Kind {
				case KindString:
					conv[k] = v.String
				case KindArray, KindObject, KindNumber, KindBool:
					raw, _ := jsonEncode(v)
					var any any
					_ = json.Unmarshal(raw, &any)
					conv[k] = any
				}
			}
			msgs = append(msgs, conv)
		}
	} else {
		msgs = []map[string]any{{"role": "user", "content": prompt}}
	}

	// Build OpenAI-shape tools array from MX shape:
	//   { name, description, params: { ... json schema ... } }
	var toolDefs []map[string]any
	for _, t := range tools {
		if t.Kind != KindObject {
			continue
		}
		name, _ := t.Object.Get("name")
		desc, _ := t.Object.Get("description")
		params, _ := t.Object.Get("params")
		if name.Kind != KindString {
			continue
		}
		fn := map[string]any{"name": name.String}
		if desc.Kind == KindString {
			fn["description"] = desc.String
		}
		if params.Kind == KindObject {
			raw, _ := jsonEncode(params)
			var p any
			_ = json.Unmarshal(raw, &p)
			fn["parameters"] = p
		} else {
			fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		toolDefs = append(toolDefs, map[string]any{"type": "function", "function": fn})
	}

	body := map[string]any{
		"model":      model,
		"messages":   msgs,
		"max_tokens": maxTokens,
	}
	if len(toolDefs) > 0 {
		body["tools"] = toolDefs
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyBytes))
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
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Value{}, err
	}
	if len(parsed.Choices) == 0 {
		return StringValue(""), nil
	}
	m := parsed.Choices[0].Message

	if len(m.ToolCalls) > 0 {
		out := NewOrderedMap()
		var calls []Value
		for _, tc := range m.ToolCalls {
			c := NewOrderedMap()
			c.Set("id", StringValue(tc.ID))
			c.Set("name", StringValue(tc.Function.Name))
			argsParsed, err := jsonDecode([]byte(tc.Function.Arguments))
			if err != nil {
				c.Set("arguments", StringValue(tc.Function.Arguments))
			} else {
				c.Set("arguments", argsParsed)
			}
			calls = append(calls, ObjectValue(c))
		}
		out.Set("tool_calls", ArrayValue(calls))
		if m.Content != "" {
			out.Set("content", StringValue(m.Content))
		}
		return ObjectValue(out), nil
	}
	return StringValue(m.Content), nil
}

// aiCompleteGemini calls Google's Generative Language API.
// Reads GEMINI_API_KEY (or GOOGLE_API_KEY) from the environment.
func aiCompleteGemini(prompt, model string, maxTokens int) (Value, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.complete with provider=gemini requires GEMINI_API_KEY or GOOGLE_API_KEY")
	}
	if model == "" || model == "gpt-4o-mini" {
		model = "gemini-2.0-flash"
	}
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": maxTokens,
		},
	})
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
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
		return Value{}, fmt.Errorf("gemini ai.complete failed (%d): %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Value{}, err
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return StringValue(""), nil
	}
	return StringValue(parsed.Candidates[0].Content.Parts[0].Text), nil
}

// openAICompatProvider describes a provider that speaks the OpenAI
// /v1/chat/completions wire format. We only need to know where to POST,
// which env var holds the key, and what model to default to.
type openAICompatProvider struct {
	Name         string // user-facing name
	BaseURL      string // full URL including /chat/completions
	EnvKey       string // environment variable that holds the API key
	DefaultModel string // used when the user passes no model (or the OpenAI default)
	NoAuth       bool   // for local Ollama
}

// openAICompatProviders is the dispatch table for OpenAI-compatible
// providers. Adding a new one is two lines: a name and an
// openAICompatProvider entry. Today: xAI Grok, Mistral, DeepSeek, Groq,
// OpenRouter, Together AI, and a local Ollama instance.
var openAICompatProviders = map[string]openAICompatProvider{
	"grok": {
		Name:         "grok",
		BaseURL:      "https://api.x.ai/v1/chat/completions",
		EnvKey:       "XAI_API_KEY",
		DefaultModel: "grok-3",
	},
	"xai": {
		Name:         "grok",
		BaseURL:      "https://api.x.ai/v1/chat/completions",
		EnvKey:       "XAI_API_KEY",
		DefaultModel: "grok-3",
	},
	"mistral": {
		Name:         "mistral",
		BaseURL:      "https://api.mistral.ai/v1/chat/completions",
		EnvKey:       "MISTRAL_API_KEY",
		DefaultModel: "mistral-large-latest",
	},
	"deepseek": {
		Name:         "deepseek",
		BaseURL:      "https://api.deepseek.com/v1/chat/completions",
		EnvKey:       "DEEPSEEK_API_KEY",
		DefaultModel: "deepseek-chat",
	},
	"groq": {
		Name:         "groq",
		BaseURL:      "https://api.groq.com/openai/v1/chat/completions",
		EnvKey:       "GROQ_API_KEY",
		DefaultModel: "llama-3.3-70b-versatile",
	},
	"openrouter": {
		Name:         "openrouter",
		BaseURL:      "https://openrouter.ai/api/v1/chat/completions",
		EnvKey:       "OPENROUTER_API_KEY",
		DefaultModel: "openai/gpt-4o-mini",
	},
	"together": {
		Name:         "together",
		BaseURL:      "https://api.together.xyz/v1/chat/completions",
		EnvKey:       "TOGETHER_API_KEY",
		DefaultModel: "meta-llama/Llama-3.3-70B-Instruct-Turbo",
	},
	"ollama": {
		Name:         "ollama",
		BaseURL:      "http://localhost:11434/v1/chat/completions",
		DefaultModel: "llama3.2",
		NoAuth:       true, // local instance, no key required
	},
}

// aiCompleteOpenAICompat handles every provider in openAICompatProviders.
// They all speak the OpenAI /chat/completions wire format, so the only
// per-provider thing is the URL, the env var, and the default model.
func aiCompleteOpenAICompat(p openAICompatProvider, prompt, model string, maxTokens int) (Value, error) {
	apiKey := ""
	if !p.NoAuth {
		apiKey = os.Getenv(p.EnvKey)
		if apiKey == "" {
			return Value{}, fmt.Errorf("ai.complete with provider=%s requires %s", p.Name, p.EnvKey)
		}
	}
	if model == "" || model == "gpt-4o-mini" {
		model = p.DefaultModel
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequest("POST", p.BaseURL, bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	if !p.NoAuth {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
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
		return Value{}, fmt.Errorf("%s ai.complete failed (%d): %s", p.Name, resp.StatusCode, string(raw))
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

// aiCompleteAnthropic calls Anthropic's /v1/messages endpoint. The model
// defaults to a recent Claude model if the user passed the OpenAI default.
func aiCompleteAnthropic(prompt, model string, maxTokens int) (Value, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.complete with provider=anthropic requires ANTHROPIC_API_KEY")
	}
	if model == "" || model == "gpt-4o-mini" {
		model = "claude-haiku-4-5-20251001"
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
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
		return Value{}, fmt.Errorf("anthropic ai.complete failed (%d): %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Value{}, err
	}
	for _, c := range parsed.Content {
		if c.Type == "text" {
			return StringValue(c.Text), nil
		}
	}
	return StringValue(""), nil
}

// ai.stream(prompt, on_chunk, opts?) streams an LLM completion. on_chunk
// is called once per delta with the new piece of text. Returns the full
// concatenated response when done. OpenAI provider only for now —
// Anthropic streaming uses a different event format and can be added
// in a follow-up.
func builtinAIStream(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("ai.stream(prompt, on_chunk, opts?) requires (string, function)")
	}
	prompt := args[0].String
	onChunk := args[1].Function
	model := "gpt-4o-mini"
	maxTokens := 512
	provider := "openai"
	if len(args) > 2 && args[2].Kind == KindObject {
		opts := args[2].Object
		if v, ok := opts.Get("model"); ok && v.Kind == KindString {
			model = v.String
		}
		if v, ok := opts.Get("max_tokens"); ok && v.Kind == KindNumber {
			maxTokens = int(v.Number)
		}
		if v, ok := opts.Get("provider"); ok && v.Kind == KindString {
			provider = strings.ToLower(v.String)
		}
	}
	// Resolve provider — default OpenAI, or any OpenAI-compatible one.
	url := "https://api.openai.com/v1/chat/completions"
	apiKey := os.Getenv("OPENAI_API_KEY")
	envKey := "OPENAI_API_KEY"
	noAuth := false
	if cfg, ok := openAICompatProviders[provider]; ok {
		url = cfg.BaseURL
		envKey = cfg.EnvKey
		noAuth = cfg.NoAuth
		if model == "" || model == "gpt-4o-mini" {
			model = cfg.DefaultModel
		}
		apiKey = ""
		if !noAuth {
			apiKey = os.Getenv(envKey)
		}
	}
	if !noAuth && apiKey == "" {
		return Value{}, fmt.Errorf("ai.stream with provider=%s requires %s", provider, envKey)
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"stream":     true,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	if !noAuth {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return Value{}, fmt.Errorf("ai.stream failed (%d): %s", resp.StatusCode, string(raw))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var full strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if len(event.Choices) == 0 {
			continue
		}
		chunk := event.Choices[0].Delta.Content
		if chunk == "" {
			continue
		}
		full.WriteString(chunk)
		if _, err := i.callFunction(nil, onChunk, []Value{StringValue(chunk)}); err != nil {
			return Value{}, err
		}
	}
	return StringValue(full.String()), nil
}

// ai.vision(prompt, images, opts?) — multimodal completion. Each image
// is either a URL or a base64 data: URL. Returns the assistant's reply
// as a string.
//
//	let r = ai.vision("Describe this", [
//	  "https://example.com/photo.jpg",
//	  "data:image/jpeg;base64," + base64_encode(read_file("./local.jpg"))
//	])
func builtinAIVision(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindArray {
		return Value{}, fmt.Errorf("ai.vision(prompt, images, opts?) requires (string, array)")
	}
	prompt := args[0].String
	model := "gpt-4o-mini"
	maxTokens := 512
	if len(args) > 2 && args[2].Kind == KindObject {
		if v, ok := args[2].Object.Get("model"); ok && v.Kind == KindString {
			model = v.String
		}
		if v, ok := args[2].Object.Get("max_tokens"); ok && v.Kind == KindNumber {
			maxTokens = int(v.Number)
		}
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.vision requires OPENAI_API_KEY")
	}

	// Build the multipart user message: text + each image as image_url.
	parts := []map[string]any{{"type": "text", "text": prompt}}
	for _, img := range args[1].Array {
		if img.Kind != KindString {
			continue
		}
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]string{"url": img.String},
		})
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   []map[string]any{{"role": "user", "content": parts}},
	})

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 90 * time.Second}
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
		return Value{}, fmt.Errorf("ai.vision failed (%d): %s", resp.StatusCode, string(raw))
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

// ai.similarity(a, b) — cosine similarity between two embedding vectors.
// Returns a number in [-1, 1] where 1 means identical direction.
//
//	let a = ai.embed("dog")
//	let b = ai.embed("puppy")
//	let s = ai.similarity(a, b)   // ~0.85
func builtinAISimilarity(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindArray || args[1].Kind != KindArray {
		return Value{}, fmt.Errorf("ai.similarity(a, b) requires two number arrays")
	}
	a, b := args[0].Array, args[1].Array
	if len(a) != len(b) {
		return Value{}, fmt.Errorf("ai.similarity: vectors must be the same length (%d vs %d)", len(a), len(b))
	}
	if len(a) == 0 {
		return NumberValue(0), nil
	}
	var dot, normA, normB float64
	for i := range a {
		if a[i].Kind != KindNumber || b[i].Kind != KindNumber {
			return Value{}, fmt.Errorf("ai.similarity: all elements must be numbers")
		}
		dot += a[i].Number * b[i].Number
		normA += a[i].Number * a[i].Number
		normB += b[i].Number * b[i].Number
	}
	if normA == 0 || normB == 0 {
		return NumberValue(0), nil
	}
	return NumberValue(dot / (math.Sqrt(normA) * math.Sqrt(normB))), nil
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

// ai.image(prompt, opts?) — OpenAI DALL-E image generation. Returns
// `{ url }` by default, or `{ b64 }` if opts.format == "b64_json".
//
//	let img = ai.image("a cat skydiving in 1920s style", {
//	  size: "1024x1024",     // also 256x256, 512x512, 1792x1024, 1024x1792
//	  model: "dall-e-3",     // or "dall-e-2"
//	  quality: "hd"          // standard | hd
//	})
//	return redirect(img.url)
func builtinAIImage(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("ai.image(prompt, opts?) requires a prompt string")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.image requires OPENAI_API_KEY")
	}
	model := "dall-e-3"
	size := "1024x1024"
	quality := "standard"
	format := "url"
	if len(args) > 1 && args[1].Kind == KindObject {
		opts := args[1].Object
		if v, ok := opts.Get("model"); ok && v.Kind == KindString {
			model = v.String
		}
		if v, ok := opts.Get("size"); ok && v.Kind == KindString {
			size = v.String
		}
		if v, ok := opts.Get("quality"); ok && v.Kind == KindString {
			quality = v.String
		}
		if v, ok := opts.Get("format"); ok && v.Kind == KindString {
			format = v.String
		}
	}
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"prompt":          args[0].String,
		"n":               1,
		"size":            size,
		"quality":         quality,
		"response_format": format,
	})
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/images/generations", bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 120 * time.Second} // image gen is slow
	resp, err := client.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return Value{}, fmt.Errorf("ai.image failed (%d): %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	if len(parsed.Data) > 0 {
		if parsed.Data[0].URL != "" {
			out.Set("url", StringValue(parsed.Data[0].URL))
		}
		if parsed.Data[0].B64JSON != "" {
			out.Set("b64", StringValue(parsed.Data[0].B64JSON))
		}
	}
	return ObjectValue(out), nil
}

// ai.transcribe(audio_path, opts?) — speech-to-text via OpenAI Whisper.
// Returns the transcription as a string. Supports mp3, mp4, wav, webm,
// m4a, ogg, flac up to 25 MB.
//
//	let text = ai.transcribe("./meeting.mp3", { language: "en" })
func builtinAITranscribe(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("ai.transcribe(audio_path, opts?) requires a path")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return Value{}, fmt.Errorf("ai.transcribe requires OPENAI_API_KEY")
	}
	audioBytes, err := os.ReadFile(args[0].String)
	if err != nil {
		return Value{}, err
	}
	model := "whisper-1"
	language := ""
	if len(args) > 1 && args[1].Kind == KindObject {
		opts := args[1].Object
		if v, ok := opts.Get("model"); ok && v.Kind == KindString {
			model = v.String
		}
		if v, ok := opts.Get("language"); ok && v.Kind == KindString {
			language = v.String
		}
	}

	// multipart/form-data body. We hand-build it because the helpers
	// in mime/multipart pull in extra plumbing we don't need elsewhere.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("model", model)
	if language != "" {
		_ = w.WriteField("language", language)
	}
	fp, err := w.CreateFormFile("file", filepath.Base(args[0].String))
	if err != nil {
		return Value{}, err
	}
	fp.Write(audioBytes)
	w.Close()

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/audio/transcriptions", &body)
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return Value{}, fmt.Errorf("ai.transcribe failed (%d): %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Value{}, err
	}
	return StringValue(parsed.Text), nil
}

// builtinEval — `eval(source, opts?)` parses and runs a string of MX Script
// source in a fresh interpreter, captures everything print/println/write
// produced, enforces a wall-clock timeout, and returns a structured result:
//
//	{
//	  ok:          true,            // false if parse or runtime error
//	  output:      "Hello, world\n",// captured stdout
//	  error:       null,            // string when ok=false
//	  duration_ms: 12,
//	  timed_out:   false
//	}
//
// The eval'd program runs with the same builtin surface as the host (no
// hard sandboxing in v1). It does NOT start an HTTP server even if the
// source contains `server { }` or route declarations — Exec only evaluates
// top-level statements; routes get registered into the throwaway interpreter
// and discarded.
//
// Options:
//
//	timeout_ms: number  default 5000
//
// This is the primitive that powers https://try.mxscript.com — the
// browser-based playground, written in MX Script itself.
func builtinEval(i *Interpreter, args []Value) (Value, error) {
	src, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}

	timeout := 5 * time.Second
	if len(args) >= 2 && args[1].Kind == KindObject {
		if v, ok := args[1].Object.Get("timeout_ms"); ok && v.Kind == KindNumber {
			timeout = time.Duration(v.Number) * time.Millisecond
			if timeout <= 0 {
				timeout = 5 * time.Second
			}
		}
	}

	result := func(ok bool, output, errMsg string, durationMs int64, timedOut bool) Value {
		o := NewOrderedMap()
		o.Set("ok", BoolValue(ok))
		o.Set("output", StringValue(output))
		if errMsg == "" {
			o.Set("error", NullValue())
		} else {
			o.Set("error", StringValue(errMsg))
		}
		o.Set("duration_ms", NumberValue(float64(durationMs)))
		o.Set("timed_out", BoolValue(timedOut))
		return ObjectValue(o)
	}

	prog, err := ParseSource(src)
	if err != nil {
		return result(false, "", err.Error(), 0, false), nil
	}

	child := New()
	var buf bytes.Buffer
	child.Out = &buf
	child.Err = &buf

	start := time.Now()
	doneCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				doneCh <- fmt.Errorf("panic: %v", r)
			}
		}()
		_, execErr := child.Exec(prog)
		doneCh <- execErr
	}()

	var execErr error
	timedOut := false
	select {
	case execErr = <-doneCh:
	case <-time.After(timeout):
		execErr = fmt.Errorf("execution exceeded %dms timeout", timeout.Milliseconds())
		timedOut = true
		// We can't actually kill the goroutine — the eval'd program may keep
		// running until it returns control. For a playground deployment, run
		// each request in a process with hard resource limits.
	}

	durationMs := time.Since(start).Milliseconds()
	output := buf.String()

	if execErr != nil {
		return result(false, output, execErr.Error(), durationMs, timedOut), nil
	}
	return result(true, output, "", durationMs, false), nil
}
