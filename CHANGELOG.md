# Changelog

All notable changes to MX Script are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/).

## [1.29.0] — 2026-05-03

### Added — inline `test "name" { ... }` blocks

```mx
fn add(a, b) { return a + b }

test "addition is commutative" {
  assert(add(2, 3) == 5)
  assert(add(3, 2) == 5)
}

test "negative numbers" {
  assert(add(-1, 1) == 0)
}
```

```bash
$ mx test app_test.mx
app_test.mx
  ✓ addition is commutative
  ✓ negative numbers
✓ 2 passed in 0s
```

The legacy `fn test_*() { ... }` form keeps working — both styles are
discovered by `mx test` in the same pass and run in fresh interpreter
state per test. The inline form lets the test name be a sentence
(`test "rejects malformed JWTs"`) instead of a snake_cased function
name, which reads better in failure output and CI logs.

The body is inert under `mx run` and `mx serve` so test files can sit
next to production code without affecting runtime behaviour.

[1.29.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.29.0

## [1.28.0] — 2026-05-03

### Added — `mx new react` template + `static_file()` helper

```bash
mx new react my-app
cd my-app
npm --prefix web install
MX_DEV=1 mx run app.mx       # terminal 1, :8080
npm --prefix web run dev      # terminal 2, Vite at :5173 (proxied)
```

A full-stack starter where MX owns `/api/*` and (in dev) proxies the
React app from Vite for HMR; in production it serves the prebuilt
`web/dist/` via a new `static_file(path) -> response | null` helper
with an `index.html` SPA fallback. The new builtin guesses Content-Type
from the extension and refuses path traversal.

### Fixed — `get /*` no longer eats the rest of the file

`get /*` (the documented catch-all route from the `proxy()` docstring)
was being lexed as a `/* ... */` block-comment opener, so anything
after it disappeared. The lexer now disambiguates: when the previous
non-trivia token is an HTTP method shorthand (`get`, `post`, ...) or
`group`, `/*` lexes as Slash + Star. Block comments everywhere else
keep working.

[1.28.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.28.0

## [1.27.0] — 2026-05-03

### Added — `mx build --compose` (self-hosted Postgres + Redis stack)

```bash
mx build --compose
docker compose up -d
docker compose logs -f app
```

Writes a three-file stack: `Dockerfile` (the existing builder), a
`.dockerignore`, and a `docker-compose.yml` wiring the app to
`postgres:16-alpine` + `redis:7-alpine`. `DATABASE_URL` and `REDIS_URL`
are pre-set so apps that use `sql.open(env("DATABASE_URL"))` or
`redis.open(env("REDIS_URL"))` connect with zero further config.

The volume `db-data` is named so `docker compose down` does not wipe
your local data, and the inner ports for db/cache are commented out
by default so nothing leaks to the host network.

[1.27.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.27.0

## [1.26.0] — 2026-05-03

### Added — `mx watch <path> -- <cmd>` (generic file watcher)

```bash
mx watch . -- go test ./...
mx watch src -- npm run build
mx watch . -- mx run app.mx --port 3000
mx watch app.mx -- mx audit app.mx
```

Generic on top of the existing dirHash polling loop. Each iteration
kills any in-flight child before launching the next so long-running
processes (servers, tests) get proper hot-reload semantics.

The `--` separator lets shell quotes pass through cleanly to the
inner command, matching how `xargs` and `bun --watch` handle it.

[1.26.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.26.0

## [1.25.0] — 2026-05-03

### Added — `config.load` + `config.expand` (env-aware config)

```yaml
# config.yaml — committed; secrets stay in env
db:
  dsn:  ${DATABASE_URL:-sqlite:./local.db}
  pool: 10
stripe:
  secret: ${STRIPE_SECRET_KEY}
  price:  ${STRIPE_PRICE_ID}
```

```mx
let cfg = config.load("./config.yaml")
let db  = sql.open(cfg.db.dsn)
```

- **Format-by-extension** — `.yaml` / `.yml` / `.json` / `.toml`
  pick the right parser automatically.
- **`${NAME}` and `${NAME:-default}` interpolation** runs against
  `os.LookupEnv` before parsing, so committed config files can
  carry secret references without the secrets themselves.
- **`config.expand(s)`** is the same substitution applied to any
  string — useful for templating connection strings or filenames at
  runtime.

[1.25.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.25.0

## [1.24.0] — 2026-05-03

### Added — `mx parse <file.mx>` (AST as JSON)

```
$ mx parse app.mx | jq '.Stmts[] | select(.Method) | "\(.Method) \(.Path)"'
"GET /users"
"POST /users"
```

Lexes + parses an `.mx` file and emits the parsed AST as
indented JSON to stdout. Useful for users building tooling on top
of MX — linters, refactor scripts, codemods, custom analysers,
documentation generators.

The shape is whatever Go's encoding/json reflects out of the AST
node types, including embedded position info on each node. Stable
enough for scripts to depend on within the 1.x line.

[1.24.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.24.0

## [1.23.0] — 2026-05-03

### Added — `mx logs <path>` (pretty-print JSON log lines)

```
$ mx logs prod.log
2026-05-03T12:00:00Z INFO  server started        port=8080
2026-05-03T12:00:01Z WARN  slow query             duration_ms=340 sql=SELECT *
2026-05-03T12:00:02Z ERROR db connection lost
2026-05-03T12:00:03Z DEBUG cache hit              key=users:42

$ mx logs prod.log --level=warn         # only warn + above
$ tail -f app.log | mx logs              # live mode, reads stdin
```

- **Colourised by level** — INFO cyan, WARN yellow, ERROR / FATAL
  red, DEBUG / TRACE gray.
- **Smart timestamp pickup** — looks for `time`, `ts`, `timestamp`,
  or `@timestamp`. Smart message pickup — `msg` or `message`.
- **Extras hung on the right** as `key=value` pairs, alphabetised.
- **`--level=` filter** drops entries below the threshold.
- **Plaintext passes through** unchanged so mixed log formats
  still flow.
- **Reads stdin** when no path is given so `tail -f | mx logs`
  works as a live tailer.

[1.23.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.23.0

## [1.22.0] — 2026-05-03

### Added — `proxy(target_url, request)` (reverse-proxy helper)

```mx
get /api/* {
  return json(handle_api(request))     # MX handles the API
}

get /* {
  return proxy("http://localhost:5173", request)  # forward everything else
}                                                  # to a Vite dev server
```

- **Forwards method + path + body + headers** to the upstream and
  returns the upstream response in MX's `Response` shape.
- **Hop-by-hop headers stripped** per RFC 2616 §13.5.1 in both
  directions (Connection, Keep-Alive, Transfer-Encoding, Upgrade,
  proxy-* set).
- **`X-Forwarded-For` added** so the upstream sees the original
  client IP.
- **Status, content-type, custom headers preserved** — the route
  caller's `return proxy(...)` just works.

3 tests cover method+path+body+header forwarding, hop-by-hop
stripping, and `X-Forwarded-For` injection.

[1.22.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.22.0

## [1.21.0] — 2026-05-03

### Added — `fetch_all(urls, opts?)` (parallel HTTP fan-out)

```mx
let pages = fetch_all([
  "https://api.example.com/users/1",
  "https://api.example.com/users/2",
  { url: "https://api.example.com/users/3", method: "POST", body: "..." }
])

loop pages as p {
  if (p.error != null) { println("failed:", p.error); continue }
  println(p.status, p.text)
}
```

- **Results in input order** so callers can correlate without
  carrying a key.
- **Mixed entry types** — bare strings default to GET; objects pass
  through as fetch's second arg with the URL pulled from `.url`.
- **Failure entries** get `{ status: 0, text: "", error: "..." }`
  so loops don't have to special-case successes.
- **`opts.concurrency` caps in-flight** (default 16, or len(urls)
  when smaller). Useful for hitting paginated APIs without
  ddosing yourself.
- **4 tests** including the concurrency cap (10 URLs with
  `concurrency: 2` peaks at exactly 2 in-flight on the server side).

[1.21.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.21.0

## [1.20.0] — 2026-05-03

### Added — `mx open <url-or-port>` (cross-platform browser opener)

```bash
mx open https://mxscript.com
mx open 8080                    # sugar for http://localhost:8080

# common dev workflow:
mx run app.mx & sleep 1 && mx open 8080
```

Tries the platform-native opener (`open` on macOS, `xdg-open` /
`sensible-browser` on Linux, `cmd /c start` on Windows) and falls
back to printing the URL when none is available. Detached so the
browser process survives the parent.

[1.20.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.20.0

## [1.19.0] — 2026-05-03

### Added — `assert_throws` + `assert_contains`

Two test helpers that round out the existing `assert` / `assert_eq`
surface for the most-asked test patterns:

```mx
fn test_rejects_bad_input() {
  assert_throws(fn() { num("not a number") })
  assert_throws(fn() { sql.exec(db, "BROKEN SQL") }, "should reject malformed SQL")
}

fn test_response_shape() {
  let r = ai.complete("hi", { provider: "anthropic" })
  assert_contains(r, "Hi")
  assert_contains(["a", "b", "c"], "b")
}
```

`assert_throws` passes when the wrapped fn raises; `assert_contains`
handles strings (substring) and arrays (element equality) with the
same call shape.

[1.19.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.19.0

## [1.18.0] — 2026-05-03

### Added — `mx version --check`

```
$ mx version --check
MX Script v1.18.0
✓ up to date (latest release: v1.18.0)

$ mx version --check
MX Script v1.10.0
↑ v1.18.0 available — run `mx upgrade`
```

Queries the GitHub releases API for the latest tag and compares it
to the binary's compile-time version using a semver-aware
comparison (strips the `v` prefix, parses MAJOR.MINOR.PATCH,
tolerates pre-release suffixes). Prints a `↑` nudge when an
upgrade exists, `✓` when current, `⚠` when GitHub is unreachable.

[1.18.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.18.0

## [1.17.0] — 2026-05-03

### Added — `notify.sms` (Twilio)

```mx
notify.sms("+15555550100", "Your code is " + code)
notify.sms(user.phone, "Order shipped!", { from: env("TWILIO_FROM_NUMBER") })
```

Reads `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM_NUMBER`.
Same `{ ok, status, error }` result shape as the rest of the
`notify.*` namespace so handlers stay declarative:

```mx
let r = notify.sms(user.phone, "OTP: " + code)
if (!r.ok) { metrics.counter("sms_failures") }
```

[1.17.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.17.0

## [1.16.0] — 2026-05-03

### Added — `xml.parse` + `xml.stringify`

For the legacy APIs SaaS apps still need to integrate with: SOAP-
style HTTP, RSS / Atom feeds, sitemaps, podcast metadata, Google
Search Console responses.

```mx
let parsed = xml.parse(read_file("./feed.rss"))
loop parsed.children[0].children as item {
  if (item.tag == "item") {
    let link = find(item.children, fn(c) { return c.tag == "link" })
    println(link.text)
  }
}

let doc = {
  tag: "feed",
  attrs: { xmlns: "http://www.w3.org/2005/Atom" },
  children: [
    { tag: "title", attrs: {}, text: "MX Updates", children: [] }
  ]
}
return xml(xml.stringify(doc))   // pair with the existing xml() response helper
```

Each parsed node carries `tag`, `attrs`, `text` (immediate text
content), and `children`. Mixed-content elements lose ordering
between text and children — acceptable trade for the simpler API.
Users who need full fidelity can drop to `encoding/xml`. Text
escapes (`&`, `<`, `>`) round-trip through both directions.

[1.16.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.16.0

## [1.15.0] — 2026-05-03

### Added — `mx ship` (preflight before merge)

```
$ mx ship
MX Script — preflight

  fmt    ✓
  check  ✓
  test   ✓

✓ ready to ship
```

Runs `mx fmt --check .` + `mx check **/*.mx` + `mx test` in order
and prints a tidy pass/fail summary. Stops at the first failure
unless `--keep-going`. Exits 1 on any failure so CI gates merges
on the same checks contributors run locally.

A single command answers "am I ready to push?" without remembering
three flag combinations.

[1.15.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.15.0

## [1.14.0] — 2026-05-03

### Added — `avg` / `count_by` / `partition` aggregations

```mx
let users = [
  { name: "ada", role: "admin", visits: 12 },
  { name: "bob", role: "user",  visits: 5 },
  { name: "cyd", role: "admin", visits: 8 }
]

avg(users, fn(u) { return u.visits })            // 8.33
count_by(users, fn(u) { return u.role })         // { admin: 2, user: 1 }
partition(users, fn(u) { return u.role == "admin" })
// → [ [ada, cyd], [bob] ]
```

Three new aggregation primitives that complete the dashboard-friendly
set alongside the existing `sum`, `min`, `max`, `group_by`, `unique`,
`flatten`. All four are also exposed under `arr.*` for libraries that
prefer namespacing.

[1.14.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.14.0

## [1.13.0] — 2026-05-03

### Added — `time.in_zone()` + `time.relative()`

```mx
// 1. Components in any IANA timezone (default time.* is UTC).
let parts = time.in_zone(time.now(), "America/New_York")
println(parts.hour, parts.zone, parts.iso)
// → 8 EDT 2026-05-03T08:00:00-04:00

// 2. Human-friendly elapsed strings — "just now", "5m ago",
//    "2h ago", "3d ago", "1mo ago", "1y ago", "in 30s", etc.
println(time.relative(post.created_at))
println(time.relative(reminder.fires_at))   // "in 1h"
```

`time.in_zone` returns `{ year, month, day, hour, minute, second,
weekday, zone, iso }` so callers can render a localised display
without doing the offset math themselves. `time.relative` covers
6 buckets (seconds → years) on either side of "now" so activity
feeds get readable timestamps without pulling in a date library.

[1.13.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.13.0

## [1.12.0] — 2026-05-03

### Added — `form.*` namespace (urlencoded form bodies)

```mx
form.parse("a=1&b=hello%20world&tags=x&tags=y")
// → { a: "1", b: "hello world", tags: ["x", "y"] }

form.encode({ user: "alice", count: 3 })
// → "count=3&user=alice"     # sorted keys

// Build a POST body for an API that wants form-encoded:
fetch(url, {
  method: "POST",
  body:    form.encode({ grant_type: "refresh_token", refresh_token: rt })
})
```

- **Single-value keys → strings**, multi-value keys → arrays.
- **Sorted-key encoding** so signing + caching upstream of MX sees
  byte-stable output.
- **Null values skip** in `form.encode` so `{ x: maybe }` patterns
  don't send the literal string "null".
- **Returns null on malformed input** — easier to compose with
  `if (form.parse(s) == null) ...` than try/catch.
- **7 tests** including the full round-trip and the array-expansion
  + sorted-key invariants.

[1.12.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.12.0

## [1.11.0] — 2026-05-03

### Added — `mx stats <file.mx>` (code-shape summary)

```
$ mx stats examples/saas_pro.mx
MX Script — stats examples/saas_pro.mx

  lines                  198
  comment lines          36
  routes                 14
  fn declarations        0
  middlewares            3
  top-level lets         3

  namespaces used:
    ai           1 call
    graphql      1 call
    health       2 calls
    magic_link   3 calls
    metrics      2 calls
    s3           1 call
    search       3 calls
    sql          14 calls
    stripe       3 calls
    webhooks     1 call
    ...
```

Punchy summary of an MX program: total + comment lines, route /
function / middleware / let counts, and a per-namespace call count
ranked by use. Lets users understand the shape of an unfamiliar
file at a glance and confirm after refactors that the surface
hasn't drifted.

[1.11.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.11.0

## [1.10.0] — 2026-05-03

### Added — `mx audit <file.mx>` (security checklist)

A static security check for the most common SaaS misconfigurations:

```
$ mx audit app.mx
MX Script — security audit app.mx

  ✗ ERROR  hardcoded secret prefix "sk_live_ found — move to env() or vault.get()
  ⚠ WARN   auth endpoints present but no rate_limit() / throttle middleware — vulnerable to brute-force
  • INFO   no TLS config in `server { ... }` — fine behind a reverse proxy, but raw HTTP if direct-served
  ✗ ERROR  webhook route present but no webhooks.verify_* call — anyone can spoof events

$ echo $?
1
```

Eight checks today:

1. **Hardcoded secret literals** — `sk_live_…` / `sk-…` / `AKIA…` /
   `ghp_…` / `xoxb-…` prefixes
2. **Plaintext password storage** — `INSERT INTO users` with
   `request.body.password` and no `password.hash` call
3. **Weak JWT secret** — `"dev-secret"`, `"secret"`, `"change-me"`
   used with `jwt.sign`
4. **Auth endpoints without rate limit** — `/login`, `/signup`,
   `/auth/*` routes that don't reach `rate_limit()` or a throttle
   middleware
5. **Server config without TLS** (info — fine behind a proxy)
6. **Webhook route without signature verification** — any
   `/webhook*` route that doesn't call `webhooks.verify_*`
7. **Raw cookie reads without verification**
8. **`password.hash` without rate limiting** (DoS warning — slow
   hash without throttling is a DoS vector)

Errors fail the command with exit 1 so CI can gate merges. Warnings
and infos still print but don't fail.

[1.10.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.10.0

## [1.9.0] — 2026-05-03

### Added — `mx fmt --diff`

```
$ mx fmt --diff app.mx
--- app.mx (current)
+++ app.mx (formatted)
- let x =1
- fn   foo(  ){return 1}
+ let x = 1
+ fn foo() {
+   return 1
+ }
```

Preview formatter changes without writing the file. Tiny line-based
unified-diff implementation — no third-party deps. Coloured red/green
in a terminal so the change is obvious.

Use cases: review what `mx fmt -w` would do before committing it,
spot-check a CI failure (`mx fmt --check` says "not formatted",
`mx fmt --diff` says exactly what would change).

[1.9.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.9.0

## [1.8.0] — 2026-05-03

### Added — `fetch_retry()` (resilient HTTP)

```mx
let r = fetch_retry("https://flaky-api.example.com/data", {
  max_attempts: 5,    // default 3
  delay_ms:     200   // default 200; doubles each attempt + jitter
})
```

Same surface as `fetch()` but retries 5xx responses and network
errors with exponential backoff (200ms → 400ms → 800ms …) plus up
to 50% jitter to avoid thundering-herd patterns.

- 4xx responses **never retry** (client error — retry won't help).
- 5xx and network errors retry up to `max_attempts` times.
- Final response (or last error) is returned after exhausting
  attempts so callers can still inspect the failure status.

3 tests cover: 500-then-success retried 3 times, 404 retried 0
times, always-503 retried exactly `max_attempts` times.

[1.8.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.8.0

## [1.7.0] — 2026-05-03

### Added — `mx db <dsn>` (interactive SQL REPL) + `read_line()`

```
$ mx db ./app.db
connected — type SQL ending with ; or .help for commands
sql> .tables
  notes
  users
sql> SELECT * FROM users LIMIT 3;
{"id":1,"email":"j@example.com","name":"Jassim"}
{"id":2,"email":"a@example.com","name":"Ada"}
sql> .quit
bye
```

Multi-line input until you hit `;`, JSON-formatted result rows, all
three SQL backends (SQLite / Postgres / MySQL) auto-detected from
the DSN. Implemented as a one-line shim that runs an MX program
which uses `sql.open` + a new `read_line()` builtin in a loop.

`read_line(prompt?)` is also a general-purpose stdin reader for
user-written CLIs:

```mx
let name = read_line("your name? ")
println("hello,", name)
```

[1.7.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.7.0

## [1.6.0] — 2026-05-03

### Added — `mx env` (env var status with masked secrets)

```
$ mx env
MX Script — environment

  AI
    ✓ OPENAI_API_KEY             sk-t…
    ✗ ANTHROPIC_API_KEY          (unset)
    ✗ XAI_API_KEY                (unset)
    ...

  Payments
    ✓ STRIPE_SECRET_KEY          sk_t…
    ✗ STRIPE_WEBHOOK_SECRET      (unset)

  Object storage
    ✓ AWS_ACCESS_KEY_ID          AKIA…
    ✓ AWS_SECRET_ACCESS_KEY      wJal…
    ...
```

40+ env vars grouped by purpose: AI providers, payments,
notifications, object storage, auth, OAuth, webhooks, runtime.
Values are first-4-chars + ellipsis so the output is safe to share
in bug reports — confirms a key is set without leaking it.

Defangs the most common SaaS-debugging question: "why isn't my
$THING working?" — usually the env var was misnamed or unset.

[1.6.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.6.0

## [1.5.0] — 2026-05-03

### Improved — LSP completion now offers user-defined names

Until v1.4 the completion list contained only the ~200 builtins +
language keywords + curated snippets. v1.5 extends it with every
top-level `let` / `fn` / `middleware` from the open document so:

- Typing `make_` autocompletes to your local `make_user` function
- Typing `req` offers your `require_auth` middleware
- Typing `DB_` offers your `DB_PATH` constant

Each user symbol carries a "(user-defined)" detail string so editors
visually distinguish them from stdlib functions in the dropdown.

[1.5.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.5.0

## [1.4.0] — 2026-05-03

### Added — `sh.*` shell helpers

```mx
// Just want the exit code?
if (sh.run("git", ["diff", "--quiet"]) != 0) { ... }

// Just want stdout? Throws on non-zero exit.
let count = sh.output("git", ["log", "--oneline"])

// Pipelines and conditionals via bash:
let py_files = sh.bash("find . -name '*.py' | wc -l").stdout

// Capability check: is ffmpeg installed?
if (sh.which("ffmpeg") == null) {
  return error("ffmpeg required for video processing")
}
```

Three thin wrappers + one path lookup, all delegating to the
existing `shell()` builtin so they share env / dir / timeout / stdin
opts. Picked `sh.*` instead of `shell.*` because the bare callable
`shell()` stays for back-compat.

[1.4.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.4.0

## [1.3.0] — 2026-05-03

### Added — LSP go-to-definition + find-references + document symbols

Three new LSP capabilities so MX feels like a first-class editor
experience in VS Code (and any other LSP client):

- **`textDocument/definition`** — Cmd-click any name to jump to its
  `let` / `fn` / `middleware` declaration. Whole-word matching so
  `foo` doesn't navigate to `foobar`.
- **`textDocument/references`** — list every site that uses the name
  under the cursor. Naive lexical scan (no scope modeling) but
  whole-word filtered, which catches the common "where is this
  used" question.
- **`textDocument/documentSymbol`** — outline panel populates with
  every top-level binding. Routes show as `get /users`, `post /users`
  so the panel reads like the route table; `let`s become Variable
  symbols, `fn`s Function, `middleware`s Constructor.

Wired through the existing initialize handler — the `capabilities`
response now advertises `definitionProvider`, `referencesProvider`,
and `documentSymbolProvider`.

8 new tests in `lsp/navigation_test.go` cover go-to-def for fn / let
/ middleware, prefix-safety (`foo` ≠ `foobar`), missing-name miss,
references across all sites, whole-word filtering (`user` ≠
`username`), and the document-symbols outline including routes.

[1.3.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.3.0

## [1.2.0] — 2026-05-03

### Added — three more AI providers (13 total)

```mx
ai.complete(prompt, { provider: "perplexity" })  // web-grounded answers
ai.complete(prompt, { provider: "fireworks" })   // fast Llama hosting
ai.complete(prompt, { provider: "cerebras" })    // extreme low-latency Llama
```

The provider matrix now spans:

| Provider | `provider:` value | API key env var |
|---|---|---|
| OpenAI | `openai` | `OPENAI_API_KEY` |
| Anthropic | `anthropic` | `ANTHROPIC_API_KEY` |
| Gemini | `gemini` | `GEMINI_API_KEY` |
| xAI Grok | `grok` | `XAI_API_KEY` |
| Mistral | `mistral` | `MISTRAL_API_KEY` |
| DeepSeek | `deepseek` | `DEEPSEEK_API_KEY` |
| Groq | `groq` | `GROQ_API_KEY` |
| OpenRouter | `openrouter` | `OPENROUTER_API_KEY` |
| Together | `together` | `TOGETHER_API_KEY` |
| Ollama (local) | `ollama` | (none) |
| **Perplexity** | `perplexity` | `PERPLEXITY_API_KEY` |
| **Fireworks** | `fireworks` | `FIREWORKS_API_KEY` |
| **Cerebras** | `cerebras` | `CEREBRAS_API_KEY` |

Same dispatch-table architecture as v0.54: adding the next OpenAI-
compatible provider stays one entry. Test coverage for all three is
folded into the existing `TestOpenAICompatProvidersTable` and
`TestOpenAICompatRequiresKey` round-trips.

[1.2.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.2.0

## [1.1.0] — 2026-05-03

### Added — `mx test --watch` (TDD-friendly test runner)

```bash
mx test --watch                  # rerun tests on every .mx change
mx test src --watch --cover      # cover + scoped to src/
```

Polls the test directory for `.mx` changes (same approach as
`mx run --watch`) and re-executes `mx test` in a fresh child
process on each save. The child boundary means a hung or panicking
test can't poison the watcher.

[1.1.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.1.0

## [1.0.1] — 2026-05-03

### Added — `crypto.*` namespace

Namespaced aliases for the existing crypto + encoding helpers, so
related primitives stay discoverable via tab-completion:

```mx
crypto.sha256("hello")                       // 2cf24dba5fb0a30e...
crypto.hmac_sha256(secret, payload)
crypto.aes_encrypt(plain, key)
crypto.uuid()
crypto.base64_encode(s) / crypto.base32_encode(s)
crypto.random_bytes(16) / crypto.random_string(32)
crypto.sign_cookie(secret, value) / crypto.verify_cookie(secret, signed)
```

Top-level forms (`sha256`, `hmac_sha256`, `aes_encrypt`, etc.) keep
working unchanged — pick whichever reads better.

[1.0.1]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.0.1

## [1.0.0] — 2026-05-03

🎉 **MX Script 1.0** — production-ready stable release.

### What this means

The core language is frozen for the 1.x line: syntax, type system,
standard library shapes, and CLI surface stay backwards-compatible
within 1.x. Future 1.x releases add features and fix bugs;
breaking changes wait for 2.0.

### What ships in 1.0

The 96 releases between v0.1.0 and v0.97.0 layered up to a complete
stack for shipping production SaaS apps in a single file:

**Language**
- Tree-walking interpreter + experimental bytecode VM (~2-3× on
  tight loops; behind `mx run --bytecode`)
- Closures, destructuring, match expressions, try/catch, optional
  chaining, nullish coalescing, short-circuit, spread args
- `loop` / `while` / `if-else`, `break` / `continue` / `return`
- `spawn { ... }` goroutines + channels + wait groups

**Web framework**
- Routes (shorthand `get /users { ... }` + verbose `route GET /...`)
- Middleware, route groups, static files, rate limiting (global +
  per-key), CORS, gzip, body limits, timeouts, graceful shutdown,
  TLS, signed cookies, AES-256-GCM
- Server-sent events, WebSockets (server + client), pure stdlib

**Stdlib (~220 builtins, 30+ namespaces)**
- `ai.*` — 10 providers (OpenAI, Anthropic, Gemini, Grok, Mistral,
  DeepSeek, Groq, OpenRouter, Together, Ollama) with `complete`,
  `stream`, `embed`, `vision`, `image`, `transcribe`
- `stripe.*` — checkout, customer, portal, subscription
- `webhooks.*` — Stripe, GitHub, Svix, Shopify, Slack signature
  verification
- `magic_link.*` — passwordless email auth (HMAC-signed tokens)
- `totp.*` — RFC 6238 TOTP (Google Authenticator compatible)
- `oauth.*` — Google, GitHub, Discord, LinkedIn, Microsoft
- `password.*` — PBKDF2 / Argon2id / scrypt
- `notify.*` — Slack, Discord, Resend email
- `s3.*` — pure-Go AWS SigV4 (works with R2, B2, MinIO, Spaces)
- `metrics.*` — Prometheus counters, gauges, histograms +
  `/metrics` endpoint
- `health.*` — k8s-style liveness + readiness probes
- `search.*` — SQLite FTS5 full-text search
- `graphql.*` — minimal GraphQL handler
- `time.*`, `path.*`, `fs.*` — date, path, glob utilities
- `id.*` — uuid, ulid, nanoid, snowflake
- `cron(spec, fn)` — Vixie 5-field schedules
- `rate_limit(key, max, window)` — token bucket
- `ws.connect(url)` — outbound WebSocket client
- `http.session()` — stateful client with cookie jar
- `vault.*` — AES-256-GCM encrypted secrets store
- `debug.*` — assert / invariant / unreachable / trace / dump
- Templates with `{{#if}}`, `{{#each}}`, `{{> partial}}`
- CSV (header-keyed), JSON, regex, image (resize / thumbnail /
  crop / convert), markdown, validation, scheduling, jobs,
  subprocess, Redis, MySQL, Postgres, base64/base32, RSA-style
  helpers, randomness

**Tooling**
- `mx run / init / new / build / repl / test [--cover] / bench /
  fmt / lsp / check / pkg / serve / ci / examples / help / docs /
  routes / upgrade / doctor`
- Six `mx new` templates: api / todo / chat / ai / blog / saas
- `mx pkg add github.com/...` — Git-backed package manager
- `mx check` — static analyzer (undefined idents, wrong arity,
  unused lets)
- `mx fmt` — opinionated formatter
- LSP with hover, completion, signature help, snippets,
  format-on-save, diagnostics
- `mx build --vercel / --docker / --fly / --railway / --wasm` —
  one-command deploy artifacts for the five most common targets
- `mx ci init github|gitlab` — CI workflow scaffolding
- Browser playground (`site/playground/`) running the full
  interpreter in WebAssembly

**Quality**
- Comprehensive test suite: every package, every namespace, every
  bundled example runs through `mx check` in CI
- Bytecode VM verified to produce identical results to the
  tree-walker
- AWS SigV4 verified against AWS's published canonical example
  byte-for-byte
- RFC 6455 WebSocket accept-header math verified against the RFC
  worked example

### Thanks

Created and maintained by Jassim Alkharafi. MIT licensed. PRs +
issues at https://github.com/jlkdevelop/mxscript.

[1.0.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v1.0.0

## [0.97.0] — 2026-05-03

### Added — `vault.*` (encrypted secrets store)

Lets users keep per-environment secrets out of the binary + out of
plaintext `.env` files. Secrets are AES-256-GCM encrypted at rest
with a single master key from `MX_VAULT_KEY`, so the resulting
`.vault.json` is safe to commit to source control.

```bash
# bootstrap once
export MX_VAULT_KEY=$(openssl rand -hex 32)
mx run --eval 'vault.set("stripe_key", "sk_test_xyz")'
mx run --eval 'vault.set("openai_key", env("OPENAI_API_KEY"))'
git add .vault.json && git commit
```

```mx
// app.mx — read at runtime, decrypted on access
let stripe = vault.get("stripe_key")
let openai = vault.get("openai_key")
```

- **`vault.get(key)`** — decrypt + return. GCM auth-tag failure
  produces a clear "wrong key?" error instead of returning garbage.
- **`vault.set(key, value)`** — encrypt + persist. Each value is its
  own ciphertext so adding a new secret doesn't re-encrypt the
  whole file.
- **`vault.list()`** — keys only; values never touch the response.
- **`vault.delete(key)`** — remove + re-save.
- **6 tests** including: round trip, encrypted-at-rest assertion
  (plaintext value must NOT appear in the on-disk file), missing
  master-key error, wrong-key decrypt failure, list+delete, and
  the 32-byte master-key validation.

[0.97.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.97.0

## [0.96.0] — 2026-05-03

### Added — `arr.*` namespace (mirrors `str.*`)

Same idea as v0.95's `str.*`: namespaced aliases for the array
helpers so library code can favor clarity:

```mx
arr.map([1, 2, 3], fn(n) { return n * 2 })
arr.filter(users, fn(u) { return u.subscribed })
arr.reduce(scores, fn(acc, n) { return acc + n }, 0)
arr.sort_by(posts, fn(p) { return p.created_at })
arr.unique(emails)
arr.flatten([[1,2], [3,4]])
arr.zip(names, ages)
arr.range(1, 11)            // [1..10]
```

Aliased: `len`, `map`, `filter`, `reduce`, `sort`, `sort_by`,
`reverse`, `find`, `push`, `pop`, `join`, `unique`, `flatten`,
`zip`, `range`.

[0.96.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.96.0

## [0.95.0] — 2026-05-03

### Added — `str.*` namespace (organising the existing string builtins)

The 15 most-used string helpers now have namespaced aliases so
library code can favor clarity over brevity:

```mx
// Top-level form (unchanged) — best for terse scripts
let name = upper(trim(input))

// Namespaced form — clearer in shared libraries
let name = str.upper(str.trim(input))
```

Both forms point at the same underlying functions; pick whichever
reads better in context. Aliased: `upper`, `lower`, `trim`,
`split`, `replace`, `contains`, `starts_with`, `ends_with`,
`substr`, `index_of`, `pad_left`, `pad_right`, `repeat`,
`escape_html` (= `html_escape`), `unescape_html` (= `html_unescape`).

[0.95.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.95.0

## [0.94.0] — 2026-05-03

### Added — `http.session()` (stateful client with cookie jar)

The standard `fetch()` builtin is stateless; `http.session()` returns
a session object that shares cookies + default headers across calls
— useful for SDKs that consume legacy form-login APIs:

```mx
let s = http.session({
  base_url: "https://api.example.com",
  headers:  { "X-Auth": "Bearer " + env("API_TOKEN") },
  timeout:  30
})

s.post("/login", { email: "x", password: "y" })
let me = s.get("/me")            // session cookie auto-attaches
println(me.body.name)             // JSON responses auto-decoded into .body

let cookies = s.cookies()         // inspect what got set
s.close()                         // clear the jar
```

- **Methods**: `get(path) / post(path, body?) / put(path, body?) /
  delete(path)`. Paths starting with `/` get prefixed with the
  `base_url`; absolute URLs pass through.
- **Body type detection**: objects + arrays → JSON; strings starting
  with `{` or `[` are sent as JSON; other strings as
  `application/x-www-form-urlencoded`.
- **JSON responses are auto-decoded** into the `body` field; the
  raw `text` is always present alongside.
- **`cookies()` snapshots the jar** so callers can persist auth state
  across runs (write to disk, hydrate next time).
- **`close()` resets the jar** by replacing it with a fresh one —
  convenient for tearing down test fixtures.

- **4 tests** cover the cookie-persistence round trip, default-header
  attachment, JSON body encoding (`{ count: 3 }` → `{"count":3}`),
  and close-clears-cookies.

[0.94.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.94.0

## [0.93.0] — 2026-05-03

### Added — `examples/saas_pro.mx` (capstone showcase)

A single-file SaaS app that exercises the full surface added in
v0.52→v0.92:

- **Magic-link auth** with Resend-sent emails
- **Stripe** checkout + customer portal + webhook activation
- **AI summarization** with `ai.complete`, gated to subscribers
- **FTS5 search** over user notes
- **S3 presigned uploads** for direct browser → R2 / S3 transfers
- **GraphQL API** for `me { ... }` and `notes { ... }` queries
- **Metrics** middleware counting every request, exposed at
  `/metrics`
- **k8s probes** at `/healthz` and `/readyz`
- **Per-IP rate limiting** middleware
- **Daily-digest cron** at 09:00

Roughly 150 lines of MX. No JS, no React, no build tool. The whole
file passes `mx check` cleanly and runs end-to-end with the right
env vars set. Doubles as the proof of MX's pitch: a real production
SaaS surface in a single readable file.

[0.93.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.93.0

## [0.92.0] — 2026-05-03

### Added — Examples integration test

Every bundled example in `examples/` now runs through `mx check` as
part of the test suite. CI catches drift between the language and its
showcase code: a renamed builtin, a removed function, a new keyword
collision — anything that would break a copy-paste from an example —
fails the test before reaching users.

```
$ go test ./checker/ -run EveryBundled -v
=== RUN   TestEveryBundledExamplePassesChecker
    --- PASS: agent.mx
    --- PASS: ai_providers.mx
    --- PASS: app.mx
    ... (16 examples total) ...
PASS
```

Each example runs as a subtest so when one fails, you see which file
+ line + column. Errors fail the suite; warnings (like unused-let)
stay tolerated.

[0.92.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.92.0

## [0.91.0] — 2026-05-03

### Added — `csv_records` / `csv_write_records` (header-aware CSV)

The existing `csv_parse` / `csv_stringify` are array-of-arrays
helpers; SaaS imports + exports almost always want the
header-keyed version:

```mx
let users = csv_records(read_file("./signups.csv"))
loop users as u {
  sql.exec(db, "INSERT INTO users (email, name) VALUES (?, ?)", u.email, u.name)
}

let payload = csv_write_records([
  { id: 1, email: "j@example.com" },
  { id: 2, email: "a@example.com" }
])
notify.email(admin, "Daily export", payload)
```

- **First row becomes the header** for `csv_records` and the column
  set for `csv_write_records`.
- **Tolerant of ragged rows.** Missing columns return empty strings
  instead of erroring.
- **Header order from the first row's keys** in `csv_write_records`
  so columns stay deterministic across calls.
- **Escaping is stdlib-correct.** Embedded commas, quotes, and
  newlines all round-trip cleanly via Go's `encoding/csv`.
- **5 tests** including the round-trip + the embedded-comma escape
  case.

[0.91.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.91.0

## [0.90.0] — 2026-05-03

### Added — `debug.*` (assert / invariant / unreachable / trace / dump)

```mx
debug.assert(user.subscribed, "user must be on the pro plan here")
debug.invariant(claims.exp > now() / 1000, "JWT expired before validation")
debug.unreachable("match arm should be exhaustive")

let result = debug.trace("expensive_query", fn() {
  return sql.query(db, "SELECT * FROM big_table")
})
// → [trace] expensive_query: 12.4ms

debug.dump(user, "user before save")
// → user before save = { id: 1, name: "Jassim", ... }
```

- **`assert` and `invariant` return the cond unchanged on success** so
  they compose inside expressions: `let user = debug.assert(load(id))`
  asserts the load returned non-null and binds the result.
- **`unreachable` always throws** — use to mark match arms or
  branches that should be impossible so future readers know they're
  intentional dead-ends.
- **`trace` is a throwaway profiler.** Wraps a fn(), logs elapsed
  time to stderr, returns whatever fn returned.
- **`dump` is `pp` with an optional label** so you can sprinkle it
  through long pipelines without losing context.

All five throw normal errors (not `os.Exit`) so try/catch around
them works naturally.

[0.90.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.90.0

## [0.89.0] — 2026-05-03

### Added — `graphql.handler(resolvers)` (minimal GraphQL)

Hand-rolled GraphQL parser + executor. Apollo / urql / Relay clients
that POST `{ query, variables }` to a route handler get the standard
`{ data, errors }` envelope back.

```mx
let handler = graphql.handler({
  Query: {
    user: fn(args, ctx) {
      return sql.query_one(db, "SELECT * FROM users WHERE id = ?", args.id)
    },
    users: fn(args, ctx) {
      return sql.query(db, "SELECT * FROM users LIMIT ?", args.limit ?? 50)
    }
  },
  Mutation: {
    create_user: fn(args, ctx) {
      let r = sql.exec(db, "INSERT INTO users (name) VALUES (?)", args.name)
      return { id: r.last_insert_id, name: args.name }
    }
  },
  User: {
    posts: fn(parent, args, ctx) {
      return sql.query(db, "SELECT * FROM posts WHERE user_id = ?", parent.id)
    }
  }
})

post /graphql { return handler(request.body) }
```

Supported:
- `query` and `mutation` operations
- Fields with arguments (numbers, strings, bools, nulls, arrays, objects, enums)
- Nested selection sets walked through resolvers per type
- **Variables**: `query Q($id: Int!) { user(id: $id) { name } }` with the
  `variables` payload bound at runtime
- **Aliases**: `current: me { name }`
- Type-keyed resolvers triggered by `__typename` on the parent value
- Apollo-style `{ data, errors }` response envelope; parse errors land
  in `errors[].message` so the client can display them

Skipped (deliberate; "minimal" not "complete"):
- Fragments, interfaces, unions, directives
- Introspection (`__schema`, `__type`)
- Subscriptions
- Schema validation against an SDL — types are duck-typed

7 tests cover query parsing (named + anonymous), mutation parsing,
aliases, end-to-end execution including nested array-of-objects
resolution, missing-resolver behavior (returns null per Apollo
convention), variables substitution, and the parse-error envelope.

[0.89.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.89.0

## [0.88.0] — 2026-05-03

### Added — `health.*` (k8s-flavoured liveness / readiness probes)

```mx
get /healthz { return health.live() }   // 200 once the server is up

get /readyz {
  return health.ready({
    database: fn() { sql.query_one(db, "SELECT 1") != null },
    redis:    fn() { redis.get(r, "ping") != null },
    queue:    fn() { return jobs.stats(q).pending < 10000 }
  })
}
```

- **Conventions baked in.** `health.live()` returns 200 whenever the
  process is alive enough to handle HTTP. `health.ready()` runs each
  check fn and returns 200 only when all pass; otherwise 503 with
  per-check status — dashboards can pinpoint which dependency is
  down without grepping logs.
- **Throwing checks count as failures.** A check fn that errors out
  is recorded with the error message in the body so debugging
  doesn't require server-side log access.
- **Truthy/falsy unification.** Returns `false` / `null` mark the
  check as failed; anything else (a row, a non-empty array, a number)
  marks it ok. Lets check fns be one-liners.
- **4 tests** cover live always-200, ready all-pass round-trip,
  degraded 503 with mixed pass/fail, and the throwing-fn path.

[0.88.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.88.0

## [0.87.0] — 2026-05-03

### Added — `ws.connect(url)` outbound WebSocket client

Server-side WS routes (`ws /chat { ... }`) have been around since
v0.30; this release adds the missing other half — an outbound
client for consuming remote WebSocket feeds:

```mx
let stream = ws.connect("wss://api.example.com/events", {
  headers: { authorization: "Bearer " + env("API_TOKEN") }
})

spawn {
  while true {
    let msg = stream.recv()
    pubsub.publish("events", msg)
  }
}
```

- **Supports `ws://` and `wss://`** — TLS dial uses `crypto/tls`
  with auto-detected SNI from the host portion.
- **RFC 6455–correct masking.** Client frames are masked per spec
  (server frames must NOT be); the existing `WSConn` was extended
  with a `clientSide` flag that flips `writeFrame`'s behavior.
- **The accept-header math is verified** against the RFC's worked
  example: key `dGhlIHNhbXBsZSBub25jZQ==` →
  `s3pPLMBiTxaQ9kYGzzhZRbK+xOo=`. Strongest possible cross-check.
- **Reuses the existing `ReadMessage` reader** so the same code
  path handles both server-side and client-side incoming frames,
  including ping/pong, close, and continuation frames.
- **3 tests** including a real end-to-end roundtrip: an httptest
  server upgrades through the existing `upgradeWebSocket`, the
  outbound `DialWebSocket` connects, sends a text frame, and reads
  the echoed reply.

[0.87.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.87.0

## [0.86.0] — 2026-05-03

### Improved — README feature matrix reflects v0.52 → v0.85

The README's "What ships in the box" table is now exhaustive — every
namespace and capability added in the v0.52→v0.85 push is in there:
10 AI providers, Stripe payments, webhook signature verification for
5 senders, magic-link / TOTP auth, S3-compatible object storage,
Prometheus metrics + `/metrics` endpoint, FTS5 search, cron, the
expanded VM with ~2-3× perf, image thumbnail/crop, time/path/fs
utilities, ULID/NanoID/Snowflake IDs, deploy generators for Docker /
Fly / Railway / Vercel / browser, terminal docs viewer, bundled
example browser. Single-screen overview of the language as it
stands.

[0.86.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.86.0

## [0.85.0] — 2026-05-03

### Added — `mx examples` (browse 16 bundled examples)

```
$ mx examples
Bundled examples (16):

  agent               a tool-calling AI agent in 60 lines.
  ai_providers        every AI provider MX Script speaks today.
  app                 a tour of MX Script as a web framework.
  blog                server-rendered blog with the v0.55 template engine.
  chat                a real-time chat app in 80 lines.
  cron                Vixie-cron scheduling for daily / weekly / hourly tasks.
  ...

$ mx examples show webhooks    # cat the source, no checkout needed
$ mx examples copy stripe .    # write stripe.mx into the current dir
```

- **`embed.FS` bundling.** Every `.mx` example in the repo is
  compiled into the binary at build time, so the command works from
  any installed `mx` — not just inside a checkout.
- **Auto-extracted summaries.** The first `// name.mx — <description>`
  comment from each file becomes its blurb in the listing — keep
  example headers rich and discovery improves automatically.
- **One-shot copy** drops a working .mx file in the current dir
  ready to `mx run`.

[0.85.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.85.0

## [0.84.0] — 2026-05-03

### Added — `mx help <topic>` and `mx docs` (built-in docs viewer)

```
$ mx help ai.complete

  ai.complete(prompt, opts?) -> string|object

  LLM completion across 10 providers. opts: provider, model, max_tokens, tools, messages.

$ mx help json_strngify
no docs for "json_strngify" (did you mean "json_stringify"?)

$ mx docs
Builtins (201):

  ai.*
    ai.complete(prompt, opts?) -> string|object
    ai.embed(text) -> array
    ai.image(prompt, opts?) -> { url, b64 }
    ...

  stripe.*
    stripe.checkout(price_id, opts?) -> { url, id }
    stripe.customer_create(email, opts?) -> { id, email }
    ...

  (top-level)
    fetch(url, opts?) -> { status, headers, body, text }
    json_stringify(value, pretty?) -> string
    ...
```

- **`mx help <topic>`** prints the curated signature + summary for
  any builtin or namespace key. Powered by the same description
  table the LSP uses for hover, so docs stay in sync.
- **`mx docs`** (alias `mx help` with no args) lists everything
  grouped by namespace prefix — `ai.*`, `stripe.*`, `time.*`, etc. —
  so you can scan the whole stdlib in one screen.
- **Typo suggestions.** `mx help json_strngify` triggers a
  Levenshtein-2 close-match probe and suggests the right name.
- **35 new namespace doc entries** filled in: `ai.complete`,
  `ai.stream`, `ai.embed`, `ai.vision`, `ai.similarity`, `jwt.*`,
  `sql.*`, `redis.*`, `oauth.*`, `password.*`, `session.*`,
  `queue.*`, `pubsub.*`. Round trips through `mx help` for every
  major namespace.

[0.84.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.84.0

## [0.83.0] — 2026-05-03

### Added — `image.thumbnail` + `image.crop` + `mx new --list`

#### Image processing

```mx
// Avatar — preserve aspect, fit in 256×256.
let thumb = image.thumbnail(file_bytes, 256)

// Profile-banner crop — top-left origin.
let banner = image.crop(file_bytes, 0, 100, 1200, 300)

return image.convert(thumb, "jpeg", 75)
```

`image.thumbnail` is a thin convenience over the existing `resize`
that picks the right dimension automatically: portraits fit by
height, landscapes by width, squares by both. Skips the resize
entirely when the image is already small enough.

`image.crop` clamps to source bounds so out-of-range coordinates
return the available subregion instead of a panic.

Both forward `format` and `quality` opts to the existing
`encodeImage` helper.

#### `mx new --list`

```
$ mx new --list
Available templates:

  api    REST API with grouped routes + OpenAPI spec + Swagger UI
  todo   Full-stack todo API with KV persistence + JWT auth
  chat   Real-time chat with WebSockets + sessions + browser client
  ai     AI agent with tool calling (3 tools, 5-turn loop)
  blog   SSR blog with SQLite + markdown posts + admin
  saas   Full SaaS starter — magic-link auth + Stripe + metrics + cron + admin
```

Same names + descriptions you'd see in the README, but without
leaving the terminal. `mx new` (with no args) does the same thing.

[0.83.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.83.0

## [0.82.0] — 2026-05-03

### Added — VM lowers `break` and `continue`

Loops with early-exit / skip-iteration semantics now compile cleanly:

```mx
loop items as n {
  if n.archived { continue }
  if found_match(n) { break }
  process(n)
}
```

Per-`Compiled` loop-frame stack tracks `breakJumps` and `contJumps`
slot indices; the loop builder patches them when the body finishes.
Both `WhileStmt` and `LoopStmt` participate. Nested loops use the
innermost frame so `break` inside an inner loop only exits that
loop, matching tree-walker semantics.

Refuses to compile top-level `break` / `continue` (no enclosing
loop frame), so the tree-walker's runtime-error path fires for
malformed programs instead of producing surprising VM behaviour.

This concludes the VM coverage push that started at v0.52. The
stack machine now lowers virtually every statement and expression
shape MX programs use:

- v0.52  expressions (arithmetic, comparison, identifiers)
- v0.53  let / = / if / while
- v0.62  function bodies, calls, return, member access
- v0.71  array / object literals, indexed reads, loop
- v0.80  `&&` / `||` / `??` short-circuit
- v0.81  `?.` optional chaining
- v0.82  `break` / `continue`

Falls back: destructuring lets, try / catch, spread args, complex
assignments (`a.b = c`, `a[0] = c`).

[0.82.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.82.0

## [0.81.0] — 2026-05-03

### Added — VM lowers `?.` optional chaining

```mx
let city = user?.profile?.city ?? "unknown"   // entirely VM-compiled
```

New `OpJumpIfNullKeep` opcode peeks the top of the stack and, if it's
null, jumps over the field access while leaving the null on the
stack as the result of the chain. Combined with the existing
`OpGetField` and v0.80's `OpNullishJump`, optional-chain expressions
compile cleanly without dropping into the tree-walker.

This was the last common expression form still falling back. The VM
now lowers virtually every shape day-to-day MX programs use.

[0.81.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.81.0

## [0.80.0] — 2026-05-03

### Added — VM lowers `&&`, `||`, `??` short-circuit

Three new opcodes that peek-and-conditionally-pop, so the VM can
compile short-circuit operators without re-evaluating the right side
when the left determines the result:

- `OpAndJump` — peek; if falsy, jump (leave value); else pop, fall through
- `OpOrJump`  — peek; if truthy, jump (leave value); else pop, fall through
- `OpNullishJump` — peek; if non-null, jump (leave value); else pop, fall through

```mx
let user = users["jassim"] ?? default_user        // VM-compiled
let safe = trusted && expensive_check()           // expensive_check NOT called when trusted is false
let role = current_role || "viewer"               // VM-compiled
```

Combined with v0.71's array/object/loop coverage and v0.62's
function-body lowering, the VM now lowers virtually every expression
shape MX programs use day-to-day. Optional chaining (`?.`) is the
last common construct still falling back.

- **9 VM tests** cover all three operators in both branches
  (truthy left, falsy left), plus the critical short-circuit
  guarantee that the right side doesn't evaluate when the left
  determines the result (uses `nonexistent_should_not_eval` as a
  tripwire — if the VM ran it, the test would fail with
  "undefined identifier" instead of returning the expected value).

[0.80.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.80.0

## [0.79.0] — 2026-05-03

### Added — `s3.*` (pure-Go AWS Signature V4)

Five-call S3 namespace that works with **every S3-compatible store**:
AWS S3, Cloudflare R2, Backblaze B2, DigitalOcean Spaces, MinIO,
Wasabi, anything else that speaks the protocol. No external SDK —
SigV4 is implemented directly so the deploy artifact stays small.

```mx
s3.put("my-bucket", "users/123/avatar.png", file_bytes, {
  content_type: "image/png"
})

let body = s3.get("my-bucket", "users/123/avatar.png")
let keys = s3.list("my-bucket", "users/123/")
s3.delete("my-bucket", "users/123/avatar.png")

// Presigned GET URL — hand to a browser to let users download
// private objects without exposing credentials.
let url = s3.presign("my-bucket", "users/123/avatar.png", { expires: 600 })
```

- **AWS S3 by default.** Reads `AWS_ACCESS_KEY_ID` /
  `AWS_SECRET_ACCESS_KEY` / `AWS_REGION` (defaults to `us-east-1`).
- **Non-AWS providers via `endpoint`** opt:

  ```mx
  let r2 = { endpoint: "https://<account>.r2.cloudflarestorage.com", region: "auto" }
  s3.put("media", "x.jpg", body, merge(r2, { content_type: "image/jpeg" }))

  let minio = { endpoint: "http://localhost:9000" }
  s3.put("test", "x.txt", "hello", minio)
  ```

- **AWS canonical-example test passes.** The signing-key
  derivation is byte-for-byte verified against AWS's published
  `c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9`
  reference value — strongest possible cross-check that the SigV4
  math is right.

- **Path-style addressing** so the same code works across providers
  without per-region cert gymnastics. Nested keys
  (`users/123/avatar.png`) preserve their structure; only individual
  segments get URL-escaped.

- **5 tests** cover the AWS canonical signing-key derivation, host
  resolution (default + R2 + MinIO), key escaping, sorted canonical
  query strings, missing-credentials error, and presign URL shape.

[0.79.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.79.0

## [0.78.0] — 2026-05-03

### Added — `pp()` pretty-printer + REPL auto-formatting

```
mx> { name: "Jassim", roles: ["admin", "dev"], posts: [{ id: 1, title: "Hello" }] }
=> {
  name: "Jassim",
  roles: ["admin", "dev"],
  posts: [
    {
      id: 1,
      title: "Hello"
    }
  ]
}
```

- **`pp(value, opts?)`** — prints a value indented and colored. Keys
  are magenta, strings cyan, numbers yellow, `true` green, `false`
  red, `null` gray, `<fn>` blue. Returns the value unchanged so it
  composes inside expressions: `let user = pp(get_user(id))` logs
  the user without changing semantics.
- **Smart inline-vs-multiline arrays.** Short numeric / string arrays
  stay on one line; arrays containing objects or longer than ~60
  chars expand. Same heuristic as JS console.log.
- **Cycle-safe.** Recursion capped at depth 10 (renders `...`).
- **Color detection** — checks `os.Stdout.Stat()` for the
  char-device bit; pipes / files get plain output automatically.
  Override with `pp(v, { colors: false })`.
- **REPL auto-uses it.** Results display through `PrettyDisplay`
  instead of one-line JSON. Multi-line input still works exactly
  the same.

[0.78.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.78.0

## [0.77.0] — 2026-05-03

### Added — `mx ci init` + one-line installer

#### `mx ci init <github|gitlab>`

Scaffolds a CI workflow that runs three checks on every push:

```yaml
# .github/workflows/ci.yml — generated
- mx fmt --check .         # formatter is clean
- mx check **/*.mx         # static analyzer passes
- mx test                  # *_test.mx files pass
```

GitHub Actions and GitLab CI both ship pre-baked. Defensive: if a
workflow file already exists at the target path, the command leaves
it alone and prints `<file> already exists`.

#### `scripts/install.sh`

Powering the one-line install in the README:

```bash
curl -fsSL https://raw.githubusercontent.com/jlkdevelop/mxscript/main/scripts/install.sh | bash
```

- **Detects OS + arch** (Darwin / Linux × amd64 / arm64).
- **Resolves the latest GitHub release** and downloads the matching
  `.tar.gz`. Pin a specific version with `MX_VERSION=v0.77.0`.
- **Falls back to `go install`** if the binary download fails and
  Go is on `$PATH` — useful on platforms where GoReleaser hasn't
  built a binary yet.
- Drops `mx` into `$HOME/.mx/bin/` (override with `INSTALL_DIR`).

[0.77.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.77.0

## [0.76.0] — 2026-05-03

### Added — `mx serve [dir] [--port N]` static file server

```bash
mx serve                        # serve cwd on :8080
mx serve dist                   # serve dist/ on :8080
mx serve site/playground --port 4000
```

- **Built on Go's `http.FileServer`** so range requests, content-type
  sniffing, ETag / If-Modified-Since handling all come for free.
- **Caddy-flavoured access log** to stdout: timestamp + status +
  method + path + duration. Doubles as a load-test surface during
  preview.
- **Defensive arg handling.** Validates the directory exists and is
  actually a directory before binding the port. Unknown flags fail
  fast.
- Pairs naturally with `mx build --wasm` — the playground is two
  commands now: `mx build --wasm && mx serve site/playground`.

[0.76.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.76.0

## [0.75.0] — 2026-05-03

### Added — `search.*` namespace (SQLite FTS5)

Production-quality full-text search backed by SQLite's built-in FTS5
engine. Zero extra dependencies — the engine ships with the
`modernc.org/sqlite` driver MX already depends on.

```mx
let db = sql.open("./app.db")

// Create the index once.
search.create(db, "posts_fts", ["title", "body"])

// Index documents (idempotent — re-indexing the same id replaces).
search.index(db, "posts_fts", post.id, {
  title: post.title,
  body:  post.body
})

// Search. BM25-ranked, supports AND/OR/NOT, NEAR proximity,
// `column:term` scoping, prefix queries — all of FTS5's surface.
let hits = search.query(db, "posts_fts", "lang AND fast", {
  limit: 20, offset: 0
})
loop hits as h { print(h.id, h.title, h.rank) }

search.delete(db, "posts_fts", post.id)
```

- **BM25 ranking out of the box.** Results come back ordered by
  relevance (lower rank = more relevant per FTS5 conventions),
  with the rank exposed as a numeric column for callers that want
  to tune.
- **Column-scoped queries** like `title:lang` only match in the
  named column — the wrapper passes user queries through unchanged
  so every FTS5 syntax feature works.
- **Re-indexing is idempotent** — `search.index` deletes the
  existing rowid before inserting, so calling it repeatedly on the
  same id doesn't duplicate the document.
- **Identifier quoting** — table names get double-quoted with
  embedded quotes doubled, so user-supplied table names can't
  break out of the SQL we control.
- **WASM stub.** Like the rest of the SQL surface, this falls back
  to a clear error in the browser build.
- **4 tests** cover create + index + ranked query, column-scoped
  queries, delete, and re-index idempotency. All run against a
  real on-disk SQLite database in a tempdir.

[0.75.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.75.0

## [0.74.0] — 2026-05-03

### Added — `id.*` namespace + object helpers

#### `id.*` — five ID schemes

```mx
id.uuid()                       // 7d4f5c8a-3a2b-4d6e-9f1a-2c8b7d6e5f4a
id.ulid()                       // 01J2A3B4C5DEFGHJKMNPQRSTV  (26 chars, time-sortable)
id.nanoid()                     // V1StGXR8_Z5jdHi6B-myT       (21 chars, URL-safe)
id.nanoid(10)                   // 7gJZ_2QK4w                  (custom length)
id.short()                      // 7gJZ_2QK                    (8 chars)
id.snowflake()                  // "1714665843123712"          (64-bit time-sortable)
```

- **ULID** uses Crockford base32 (no I/L/O/U) and packs the
  millisecond timestamp into the first 10 chars, so two IDs minted
  in the same DB query sort lexicographically by creation time.
- **NanoID** uses 64-char URL-safe alphabet — drops in anywhere
  UUIDs go but with shorter strings (21 chars vs 36).
- **Snowflake** packs ms-since-2020 + 22 random bits into a 64-bit
  ID returned as a numeric string (MX's float64 numbers can't
  represent it without precision loss).

#### Object helpers

```mx
let safe   = pick(user, ["id", "email", "name"])      // copy with only those keys
let public = omit(user, ["password_hash", "api_key"]) // copy without those keys
let cfg    = merge(defaults, overrides)               // shallow, b wins
let cfg    = deep_merge(defaults, overrides)          // recursive — descends nested objects
```

All four are copy-rather-than-mutate so chained transformations
don't surprise callers. `merge` preserves key order from `a` and
appends new keys from `b`; `deep_merge` recurses when both sides
have objects at the same key.

- **10 tests** cover ULID shape + uniqueness, NanoID default + custom
  length, short(), snowflake's all-digits invariant, pick/omit
  semantics, merge tie-breaking, deep_merge with `db.host` overridden
  but `db.port` preserved.

[0.74.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.74.0

## [0.73.0] — 2026-05-03

### Improved — friendlier "did you mean" hints on namespace typos

Top-level builtins already suggested fixes (`prntln` → `println`).
Now namespace typos do too:

```
$ mx run --eval 'ai.complte("hi")'
error: cannot call null (did you mean .complete?)

$ mx run --eval 'webhooks.verify_strpe(...)'
error: cannot call null (did you mean .verify_stripe?)

$ mx run --eval 'metrics.contr("x")'
error: cannot call null (did you mean .counter?)
```

When a `CallExpr` resolves to null and the callee was a `MemberExpr`
on an object, the runtime walks the object's keys and proposes the
closest match within Levenshtein-2. Powered by a new `suggestKey()`
helper that mirrors the existing `suggestIdentifier` for top-level
names.

Hits every namespace MX ships: `ai.*`, `stripe.*`, `webhooks.*`,
`metrics.*`, `totp.*`, `magic_link.*`, `notify.*`, `time.*`,
`path.*`, `redis.*`, `sql.*`, `oauth.*`, `jwt.*`, `pubsub.*`, `fs.*`.

[0.73.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.73.0

## [0.72.0] — 2026-05-03

### Added — `mx build --docker` / `--fly` / `--railway`

Three deploy-target generators that get an MX project from "works on
my machine" to "deployed" in two commands.

```bash
mx build --docker     # Dockerfile + .dockerignore
docker build -t my-app . && docker run -p 8080:8080 my-app

mx build --fly        # Dockerfile + .dockerignore + fly.toml
fly launch --copy-config && fly deploy

mx build --railway    # Dockerfile + .dockerignore + railway.toml
railway up
```

- **Multi-stage Dockerfile.** Builds the `mx` binary with
  `golang:1.25-alpine`, copies it into a tiny `alpine:3.19` runtime
  image, embeds the user's `.mx` files, exposes `8080`, sets
  `ENTRYPOINT ["mx", "run", "app.mx"]`. Works for any MX project
  without modification.

- **Defensive writes.** Each generator skips files that already
  exist — users can customize freely without losing their changes
  on the next build. The CLI prints `<file> already exists —
  leaving it alone` so it's obvious nothing happened.

- **Sensible deploy defaults.** Fly's config sets the smallest VM
  size (`shared-cpu-1x`, `256mb`), turns on `auto_stop_machines`,
  and forces HTTPS. Railway's config defines a healthcheck on `/`
  and `restartPolicyType = ON_FAILURE` with 3 retries.

- **`.dockerignore` ships with sane excludes** (`.git`, `.env`,
  `*.bin`, `*.db`, `mx_modules/`, `dist/`, `node_modules/`).

Combined with the existing `mx build --vercel` and `--wasm`, MX now
generates deploy artifacts for the five most common targets:
Vercel, the browser, Docker, Fly, Railway.

[0.72.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.72.0

## [0.71.0] — 2026-05-03

### Added — VM lowers `[arr]` / `{obj}` / `loop` / `[index]`

Wide-coverage push for `--bytecode`. Programs that build arrays,
objects, and iterate over collections — i.e. most real programs —
now run on the stack machine end-to-end.

```bash
$ mx bench /tmp/loop.mx                # tree-walker
loop array     5.42 us/op
$ mx bench --bytecode /tmp/loop.mx     # bytecode
loop array     3.56 us/op    (~1.5×)
```

Make-object micro-bench drops from 29.73 us/op to 14.66 us/op (2×).

- **`OpMakeArray`** pops N values + pushes `KindArray`.
- **`OpMakeObject`** pops N×2 (key, value) values + pushes `KindObject`.
  Keys come from the constant pool so encoding stays compact.
- **`OpGetIndex`** indexes arrays (numeric), objects (string), and
  strings (numeric, returns single-char). Out-of-bounds reads return
  null, matching the tree-walker.
- **`OpLength`** — pops a value and pushes its length (array
  elements, string bytes, object keys; null → 0).
- **`LoopStmt` lowering** — `loop xs as n { body }` desugars to a
  while loop with hidden synthetic temporaries (`__loop_arr_0`,
  `__loop_idx_0`, `__loop_len_0`). Nested loops use unique counters
  so siblings don't collide. Optional `loop xs as i, item { ... }`
  exposes both index and value.
- **Spread elements** in array literals still fall back (need a
  runtime concat opcode); plain `[1, 2, 3]` and `{ a: 1, b: 2 }`
  compile cleanly.
- **3 new VM tests** cover array literals, object literals, indexed
  reads, loop-over-array totals, loop-with-index variants, and a
  nested-loop sanity check (sum 1..3 × sum 10..20 = 180).

[0.71.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.71.0

## [0.70.0] — 2026-05-03

### Added — `ai.image()` (DALL-E) + `ai.transcribe()` (Whisper)

```mx
let img = ai.image("a cat skydiving in 1920s style", {
  size: "1024x1024",
  model: "dall-e-3",
  quality: "hd"
})
return redirect(img.url)

let text = ai.transcribe("./meeting.mp3", { language: "en" })
println("transcript:", text)
```

- **`ai.image(prompt, opts?)`** — DALL-E 3 by default, configurable
  via `model` (`dall-e-2` / `dall-e-3`), `size`
  (`256x256` / `512x512` / `1024x1024` / `1792x1024` / `1024x1792`),
  `quality` (`standard` / `hd`), `format` (`url` / `b64_json`).
  Returns `{ url }` by default or `{ b64 }` if format is `b64_json`.
  120-second timeout (image gen is slow).

- **`ai.transcribe(audio_path, opts?)`** — Whisper speech-to-text.
  Reads the audio file from disk and posts as multipart/form-data.
  Supports mp3, mp4, wav, webm, m4a, ogg, flac up to 25 MB. Returns
  the transcript as a string. Optional `language` hint speeds up
  detection.

- Both round out the AI namespace alongside the existing
  `ai.complete` (10 providers), `ai.stream`, `ai.vision`, `ai.embed`,
  `ai.similarity`. Eight AI primitives total.

[0.70.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.70.0

## [0.69.0] — 2026-05-03

### Added — `rate_limit()` builtin (token-bucket, per-key)

Application-level rate limiting keyed by an arbitrary string. Use it
per user, per tenant, per IP, per endpoint — whatever makes sense.

```mx
route POST /signup {
  if (!rate_limit("signup:" + request.ip, 5, 60)) {
    return status(429, { error: "too many requests, slow down" })
  }
  // ... real signup
}

route POST /messages {
  if (!rate_limit("msg:" + claims.user_id, 30, 60)) {
    return status(429, { error: "rate limited" })
  }
  // ... send the message
}
```

- **Token-bucket algorithm.** Capacity `max` tokens; refills linearly
  at `max / window_seconds` tokens/sec. First call after a long pause
  sees a full bucket. Each successful call consumes one token.
- **In-process registry.** Buckets share a global `sync.Mutex`-guarded
  map. Restarts reset every bucket — for cross-instance limits, back
  this onto Redis with `redis.incr(...)`.
- **`rate_limit_reset(key?)`** clears one bucket (or all if no key) —
  test-only escape hatch.
- **6 tests** cover the burn-budget-then-deny path, key independence,
  timing-accurate refill (waits 150ms, asserts a token reappeared),
  invalid-budget zero/negative handling, error reporting on bad args,
  and per-key reset.

This complements (not replaces) the existing global `server { rate_limit
{ ... } }` config. The server-level limit is a defense for the whole
app; the new builtin lets individual routes apply finer-grained
controls.

[0.69.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.69.0

## [0.68.0] — 2026-05-03

### Added — `time.*`, `path.*`, `fs.glob` stdlib expansion

The constantly-needed builtins every modern language ships. ~20
new functions across three namespaces.

#### `time.*`

```mx
let t = time.parse("2026-05-03T12:00:00Z")
let tomorrow = time.add(t, "24h")
let elapsed  = time.diff(t, time.now())            // seconds
let weekday  = time.weekday(t)                     // "Sunday"

println(time.format(t, "2006-01-02 15:04:05"))     // Go layout strings
println(time.year(t), time.month(t), time.day(t))  // 2026 5 3
```

`time.parse` accepts RFC 3339, ISO 8601, common date forms, RFC 1123,
RFC 822 — returns `null` (not throws) on garbage so you can compose
with `if (time.parse(s) == null)`. `time.add` takes Go-format
duration strings (`"1h"`, `"24h"`, `"1500ms"`).

#### `path.*`

```mx
path.join("/a", "b", "c.txt")  // "/a/b/c.txt"
path.dir("/a/b/c.txt")         // "/a/b"
path.base("/a/b/c.txt")        // "c.txt"
path.ext("/a/b/c.txt")         // ".txt"
path.absolute("./x")           // "/cwd/x"
```

#### `fs.glob`

```mx
fs.glob("*.mx")            // flat — current dir
fs.glob("src/**/*.go")     // recursive (depth-walking the tree)
```

`**` is supported as a glob token even though Go's stdlib
`filepath.Glob` doesn't — we walk the prefix manually so users get
the ergonomic recursive form they expect.

- 14 tests cover RFC parsing, garbage rejection, format roundtrips,
  custom layouts, `add` with durations, `diff`, weekday, six
  component extractors, path join/dir/base/ext, flat + recursive
  glob, current-time sanity, and number-required errors.

[0.68.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.68.0

## [0.67.0] — 2026-05-03

### Added — `mx new saas` template

- **A complete SaaS starter in one file.** Showcases the full
  v0.55–v0.66 feature surface end-to-end:

  ```bash
  mx new saas my-app
  cd my-app
  cp .env.example .env       # fill in Stripe + Resend keys
  mx run app.mx
  ```

  What it ships:
  - **Magic-link auth** (`magic_link.create` / `verify`)
  - **Stripe checkout** with `customer_create` + `subscription` mode
  - **Customer portal** for self-service billing management
  - **Webhook handler** (`webhooks.verify_stripe`) marks users active
  - **Prometheus `/metrics`** endpoint with per-route request counter
  - **Daily-digest cron** job at 09:00
  - **`/admin` dashboard** listing every user + plan + signup date
  - **Pricing page** + dashboard + sign-in flow

  Total: ~150 lines of MX, zero JS, zero React, zero build tool.

- **Six templates total** now ship (`api`, `todo`, `chat`, `ai`,
  `blog`, `saas`). The `mx new --help` enumerates them in the same
  order builders typically encounter them.

- **Real-checker-validated.** `mx check` now passes cleanly on the
  saas template — caught a `?:` ternary I mis-typed (MX uses
  `match cond { true => x, _ => y }` instead) and the missing
  scope-bind for the magic-link click handler. Before-shipping
  verification, in seconds.

[0.67.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.67.0

## [0.66.0] — 2026-05-03

### Added — `metrics.*` namespace + Prometheus `/metrics` endpoint

- **Three primitives covering 99% of observability needs:**

  ```mx
  metrics.counter("http_requests_total", 1, { method: "GET", path: "/users" })
  metrics.gauge("active_connections", connections.length)
  metrics.histogram("request_latency_seconds", elapsed_seconds, { path: "/users" })
  ```

- **One-line Prometheus endpoint:**

  ```mx
  route GET /metrics { return metrics.handler() }
  ```

  Output is the standard Prometheus text exposition format
  (openmetrics) — drops into Prometheus, Grafana Cloud,
  VictoriaMetrics, the Datadog Agent's openmetrics check, Honeycomb's
  OTel collector, anything else that scrapes.

- **Fixed-bucket histograms** (latency-shaped: 1ms..10s) with
  cumulative-count emission per Prometheus convention. Each
  observation also feeds `_sum` and `_count` so quantiles, average
  latency, and rates work out of the box.

- **Label sets are stable.** Two calls with `{a:1, b:2}` and
  `{b:2, a:1}` share the same time series (sorted-key fingerprint).
  Label values are escaped properly for the wire format.

- **Mismatched-kind protection.** Reusing a name as a different type
  is a programmer error — the original kind is preserved so the
  `/metrics` output stays valid even when code drifts.

- **9 tests** cover counter accumulation, value override, labels,
  gauges, histogram bucket math, type lines, response shape, label
  ordering determinism, and the kind-clash safeguard.

- **`examples/metrics.mx`** — one route per metric type plus the
  scrape endpoint, in 30 lines. Live-tested end-to-end.

[0.66.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.66.0

## [0.65.0] — 2026-05-03

### Added — `stripe.*` payments namespace

- **The four calls every SaaS app actually makes**, behind a clean
  surface that pairs with v0.56's `webhooks.verify_stripe`:

  ```mx
  let session = stripe.checkout("price_abc", {
    mode: "subscription",
    success_url: "https://app.example/welcome",
    cancel_url:  "https://app.example/pricing",
    customer_email: "alice@app.com"
  })
  return redirect(session.url)

  let cust   = stripe.customer_create("alice@app.com", { name: "Alice" })
  let portal = stripe.customer_portal(cust.id, "https://app.example/account")
  let sub    = stripe.subscription_create(cust.id, "price_abc", { trial_period_days: 7 })
  ```

- **All four read `STRIPE_SECRET_KEY`** from the environment and
  return small ordered-map results — `{ url, id }` for checkout /
  portal, `{ id, email }` for customer create, `{ id, status }` for
  subscription create. Never any `null`s on success.

- **HTTP plumbing centralised.** `stripeRequest()` handles the
  basic-auth + `application/x-www-form-urlencoded` + 30s timeout
  combo Stripe still expects in 2026, so each public helper stays
  one screen.

- **Test override hook.** `stripeBaseURLFn` is a function variable
  callers can swap to point at httptest in tests. Production
  resolves to `https://api.stripe.com/v1` unchanged.

- **5 wire-format tests** confirm checkout payload, customer-create
  payload (including `metadata[plan]=pro` form encoding), portal,
  subscription with `trial_period_days`, and missing-API-key error.

- **`examples/stripe.mx`** — full SaaS payment loop in 60 lines:
  pricing page → signup → checkout → webhook → marks user active →
  customer portal for billing management. Pairs `stripe.*` with
  `webhooks.verify_stripe` end-to-end.

[0.65.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.65.0

## [0.64.0] — 2026-05-03

### Added — browser playground at `site/playground/`

- **A complete in-browser playground** built on the v0.63 wasm
  target. Single HTML page with a textarea editor, an output pane,
  and a wired-in `Run ▶` button. Cmd/Ctrl-Enter runs the program.

- **Six bundled examples**, picked to show off the language without
  needing network IO:
  - Hello world
  - Closures + counter pattern
  - `map` / `filter` / `reduce` over a numeric array
  - `match` expression with wildcard
  - JSON encode + decode round trip
  - Tight numeric loop (showcasing the bytecode VM)

- **Drops on any static host.** Three files: `index.html`,
  `wasm_exec.js`, `mx.wasm`. Push to GitHub Pages, Vercel, Netlify,
  Cloudflare Pages, S3 — anywhere that serves files. The page only
  needs HTTP(S); `WebAssembly.instantiateStreaming` rejects
  `file://` origins.

- **Production-grade UI**. Dark GitHub-style theme, monospace code
  pane with tab support, run-time milliseconds reported in the
  status bar, error styling for failed runs, brand colour matched
  to the logo. Fully responsive (collapses to single column under
  720px).

- **`mx.wasm` is gitignored**. Build artefacts don't bloat the repo;
  the playground README documents the one-line build:

  ```bash
  mx build --wasm && cp dist/mx.wasm site/playground/
  ```

- **README pitched at the top.** New "Try it in your browser"
  section above the install steps so newcomers can play without
  cloning anything.

[0.64.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.64.0

## [0.63.0] — 2026-05-02

### Added — `mx build --wasm` (run MX in the browser)

- **The interpreter compiles to WebAssembly.** A new `cmd/mxwasm`
  entry point exposes a single JS function:

  ```js
  const { stdout, stderr, error } = window.mxRun(`
    let xs = [1, 2, 3]
    println(map(xs, fn(n) { return n * 2 }))
  `)
  ```

  Programs lex, parse, and execute in the browser exactly the same
  way they do natively. Routes register but never serve traffic
  (there's no http.Server in the wasm sandbox) — the playground
  intentionally focuses on language semantics, not network IO.

- **`mx build --wasm` ships everything users need.** Produces
  `dist/mx.wasm` (~14 MB) plus the matching `dist/wasm_exec.js` from
  the Go toolchain. Drop both into any static-hosting setup and load
  them from a page.

- **Build-tag isolation for non-browser features.** `sql.go`,
  `redis.go`, and `jobs.go` are gated behind `//go:build !js`;
  `sql_wasm.go`, `redis_wasm.go`, and `jobs_wasm.go` provide stub
  builtins that return clear errors at runtime. Same source tree,
  one `go build` flag — no fork.

- **Stdout/stderr capture** runs through the existing `Out`/`Err`
  fields on `Interpreter`, so the wasm host gets a clean string back
  instead of writing to a console the page can't read.

[0.63.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.63.0

## [0.62.0] — 2026-05-02

### Added — VM lowers function bodies + calls + return + member access

- **Function bodies now run on the bytecode VM.** When `--bytecode` is
  on, the first call to each user-defined function tries to compile
  its body and caches the result on the `Function` struct. Subsequent
  calls go straight to the VM. Bodies the compiler can't lower fall
  back to the tree-walker once and stay there (negative cache).

  ```bash
  $ mx bench /tmp/bench.mx                  # tree-walker
  loop in fn   348us/op
  $ mx bench --bytecode /tmp/bench.mx       # function body on VM
  loop in fn   178us/op   (~2× faster)
  ```

- **`OpCall` opcode** dispatches calls inside compiled programs. The
  VM compiler lowers `CallExpr` (callee + args + OpCall); at runtime,
  OpCall pops `argc` arguments and the callee, hands off to
  `Interpreter.callFunction`, then pushes the return value. Native
  builtins and user functions both work — recursion included.

- **`OpReturn` for `ReturnStmt`.** `return expr` evaluates the
  expression and halts the VM with that value on the stack — exits
  cleanly even from inside loops or conditionals. `return` with no
  value pushes null. Combined with function-body lowering, this means
  `if cond { return early } ...` now compiles in full.

- **`OpGetField` for `MemberExpr`.** `user.name`, `request.params.id`,
  `r.body.email` all run on the VM. Optional chaining (`?.`) still
  falls back so the null-guard semantics stay correct.

- **Per-function `sync.Mutex`** guards the lazy-compile path so
  concurrent goroutines (`spawn { ... }`) don't race the cache.

- **4 new VM tests** cover call dispatch, return, member access, and
  cached repeated calls. All 15 VM tests + 161 total interpreter
  tests pass.

[0.62.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.62.0

## [0.61.0] — 2026-05-02

### Added — `mx pkg` package manager

- **Six subcommands cover the full dependency lifecycle:**

  ```bash
  mx pkg init                                # scaffold mxpkg.json
  mx pkg add github.com/foo/bar              # clone + lock to current SHA
  mx pkg list                                # show deps
  mx pkg remove github.com/foo/bar
  mx pkg update [import-path]                # git pull + relock (one or all)
  mx pkg install                             # install every locked dep (post-clone)
  ```

- **Manifest format (`mxpkg.json`).** Stable JSON with `name`,
  `version`, and a `dependencies` map of import-path → `{ url, ref,
  entry? }`. Two-space indent, trailing newline — diffs stay
  readable across versions.

- **`import "github.com/foo/bar"` works.** The interpreter wires up
  a `PackageResolver` callback at startup so package paths route to
  `./mx_modules/foo/bar/main.mx` (or the entry file the manifest
  specifies). Relative paths (`./auth.mx`, `../shared/x.mx`) keep
  working unchanged.

- **Path normalisation accepts every form users actually paste:**
  `github.com/foo/bar`, `https://github.com/foo/bar`,
  `https://github.com/foo/bar.git`, `git@github.com:foo/bar.git` —
  all collapse to the canonical `github.com/foo/bar` key.

- **Reproducible installs.** `mx pkg install` clones each manifest
  dep at the locked SHA via `git clone` + `git reset --hard <sha>`.
  Already-installed deps at the right SHA are skipped.

- **`mx_modules/` auto-added to `.gitignore`** for both `mx init`
  and every `mx new` template — projects don't accidentally commit
  cloned dependencies.

- **10 pkg tests** covering URL normalisation, clone-URL building,
  on-disk path computation, manifest round-trip, package-vs-relative
  path heuristic, and `init` idempotency.

- **Smoke-tested live**: `mx pkg add github.com/jlkdevelop/mxscript`
  cloned the real repo and locked the current commit successfully.

[0.61.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.61.0

## [0.60.0] — 2026-05-02

### Added — `mx check` static analyzer + new randomness builtins

- **`mx check <file.mx>` finds bugs before `mx run` does.** A single
  AST pass catches the three most common classes of mistake:

  ```bash
  $ mx check app.mx
  app.mx:42:7: error: undefined identifier "respnse"
  app.mx:51:5: error: function "add" expects 2 argument(s), got 1
  app.mx:60:1: warning: unused let binding "tmp"
  3 issues (2 errors, 1 warning)
  ```

  Errors fail the build (exit 1); warnings just print. Underscore
  prefixes silence the unused warning (`let _intentional = ...`).

- **Scope-aware.** Routes auto-bind `request`. WebSocket routes
  (`ws /chat { ... }`) auto-bind `send`, `recv`, `close`. SSE routes
  auto-bind `send`. Loop variables, catch variables, destructure
  patterns, and namespaced imports (`import "./x" as x`) all bind
  correctly. Mutual recursion at the top level works (forward decls).

- **Caught real bugs in our own examples** the first time it ran —
  `chat.mx` was using `close()` (now bound), `passwordless.mx` was
  calling `base32_encode/random_bytes` (now real builtins),
  `stdlib_test.mx` had a nested fn decl the checker missed (now fixed).
  All six issues found by running the checker on `examples/*.mx`,
  all six legitimate.

- **14 checker tests** covering happy paths, undefined identifiers,
  arity errors, spread arguments, builtin recognition, route bodies
  with `request`, loop scopes, nested function declarations, mutual
  recursion, unused warnings, underscore-suppressed warnings,
  destructure binding, and namespaced imports.

### Added — randomness + base32 builtins

- **`random_string(n, alphabet?)`** — n random characters from
  `alphabet` (defaults to RFC 4648 base32). Use this for TOTP
  secrets, short IDs, invitation codes.
- **`random_bytes(n)`** — n cryptographically random bytes,
  hex-encoded (returns 2n chars).
- **`base32_encode(s)` / `base32_decode(s)`** — RFC 4648 base32
  encoding. Decode tolerates lower-case and missing padding.

All four use `crypto/rand` for the underlying entropy.

[0.60.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.60.0

## [0.59.0] — 2026-05-02

### Added — `notify.*` namespace (Slack / Discord / Resend email)

- **One-line outbound posts to the three channels every SaaS app
  wires up first.** No more copy-pasted curl recipes:

  ```mx
  notify.slack(env("SLACK_WEBHOOK"), "✅ deploy succeeded")
  notify.discord(env("DISCORD_WEBHOOK"), "build broke 💥")
  notify.email("user@example.com", "Verify your email",
    "<p>Click <a href='${link}'>here</a></p>", { html: true })
  ```

- **Rich-payload pass-through.** String messages take the simple
  shape (`{text:...}` for Slack, `{content:...}` for Discord); object
  messages pass through unchanged so users can send blocks, embeds,
  attachments, custom usernames, avatar URLs, etc.

  ```mx
  notify.discord(env("DISCORD_WEBHOOK"), {
    content: "release v1.2.3",
    embeds: [{ title: "Changelog", description: "...", color: 65280 }]
  })
  ```

- **Standard result shape.** Every `notify.*` returns
  `{ ok, status, error }` so handlers stay declarative — no
  try/catch boilerplate around outbound calls:

  ```mx
  let r = notify.slack(env("SLACK_WEBHOOK"), "deploy")
  if (!r.ok) { log.warn("slack failed:", r.error) }
  ```

- **Resend email integration.** `notify.email` posts to the Resend
  API (`RESEND_API_KEY`). Optional `from` (defaults to `RESEND_FROM`
  env or `noreply@example.com`), `html` (vs plain text), `reply_to`,
  `cc`, `bcc`.

- **6 wire-format tests** using `httptest` so the body shape, headers
  (auth bearer, content-type), and error-result construction are all
  asserted without making real network calls.

- **`examples/notify.mx`** — three routes, three channels, drop-in.

[0.59.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.59.0

## [0.58.0] — 2026-05-02

### Added — passwordless auth (`magic_link.*` + `totp.*`)

- **`magic_link.create(email, secret, opts?)` and `magic_link.verify(token, secret)`.**
  Signed, stateless, time-limited tokens for "send me a sign-in
  email" flows. No DB roundtrip needed at click time — the HMAC
  signature carries the email and expiry inline.

  ```mx
  route POST /auth/request {
    let token = magic_link.create(request.body.email, env("SECRET"), {
      expires_minutes: 15
    })
    email.send(request.body.email, "Your sign-in link",
      "Click: https://app.example/auth/click?token=" + token)
    return json({ sent: true })
  }

  route GET /auth/click {
    let email = magic_link.verify(request.query.token, env("SECRET"))
    if (email == null) {
      return status(401, { error: "invalid or expired link" })
    }
    return json({ logged_in: true, email: email })
  }
  ```

  Tampered tokens, expired tokens, and tokens signed with the wrong
  secret all return `null` — handlers stay declarative.

- **`totp.generate(secret)`, `totp.verify(code, secret, drift?)`,
  `totp.uri(account, secret, issuer?)`.** RFC 6238 TOTP, fully
  Google Authenticator / Authy / 1Password compatible. Drift defaults
  to ±1 slot (90-second window) so slow users / clock skew don't
  lock people out. Pass `drift=0` for strict 30s windows.

  ```mx
  let totp_secret = upper(base32_encode(random_bytes(20)))
  let provisioning = totp.uri("alice@app.com", totp_secret, "Acme")
  // Pass `provisioning` to a QR-code generator and the user scans it.

  if (totp.verify(request.body.code, totp_secret)) {
    return json({ logged_in: true })
  }
  ```

- **Secret normalisation.** Authenticator apps emit upper-case base32
  with no padding; users sometimes paste lower-case, with spaces, or
  with trailing `=` padding. All four forms produce the same code.

- **Constant-time comparison everywhere** (`hmac.Equal`) to defend
  signature-verification calls against timing attacks.

- **10 round-trip tests** covering happy paths, tampered emails,
  expired tokens, wrong secrets, drift windows, and base32 input
  variants.

- **`examples/passwordless.mx`** — magic-link sign-in plus optional
  TOTP 2FA enrolment, in 50 lines.

[0.58.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.58.0

## [0.57.0] — 2026-05-02

### Added — `cron()` scheduler + `mx routes` introspection

- **`cron(spec, fn)` runs a function on a Vixie-cron schedule.** Standard
  five-field expression: `"minute hour day-of-month month day-of-week"`.

  ```mx
  cron("0 9 * * 1-5", fn() { send_daily_digest() })  // 09:00 weekdays
  cron("*/5 * * * *", fn() { sweep_jobs() })         // every 5 minutes
  cron("0 0 1 * *",  fn() { roll_invoices() })       // 1st of each month
  ```

  Each field supports `*`, single values, lists (`1,5,10`), ranges
  (`1-10`), and steps (`*/5`, `9-17/2`). Day-of-month and day-of-week
  combine with OR semantics when both are restricted, matching Vixie
  cron's documented behavior. Returns a stop function:

  ```mx
  let stop = cron("* * * * *", fn() { ... })
  stop()
  ```

- **Bitmask-based matching.** Each field compiles to a `uint64` of
  allowed values, so the per-minute "does this fire?" check is one
  AND per field — fast enough that `Next()` walks minute-by-minute up
  to four years for pathological specs (`* * 29 2 *`).

- **8 cron tests** including the Vixie OR semantics, step expressions,
  range expressions, month-boundary `Next()`, and parser rejection of
  every documented invalid form.

- **`mx routes <file.mx>` lists every route a program registers**
  without booting the HTTP server. Loads, runs initialization, and
  prints the route table — useful for understanding an unfamiliar
  codebase, generating an OpenAPI spec offline, or asserting in CI
  that the route surface hasn't changed.

  ```bash
  $ mx routes examples/webhooks.mx
    POST    /webhooks/stripe
    POST    /webhooks/github
    POST    /webhooks/svix
    POST    /webhooks/shopify
    POST    /webhooks/slack

  5 routes
  ```

- **Public `Interpreter.RouteSummary() []RouteInfo`.** Embedders that
  want to build their own admin / dev tooling can introspect routes
  without poking at unexported fields.

- **`examples/cron.mx`** — heartbeat, weekday digest, monthly billing,
  weekly cleanup. Drop-in starter.

[0.57.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.57.0

## [0.56.0] — 2026-05-02

### Added — `webhooks.*` namespace
- **One-line webhook verification for the five providers most apps
  actually need.** No more hand-rolled HMAC code in route handlers,
  no more replay-attack windows left open by accident.

  ```mx
  route POST /webhooks/stripe {
    let ok = webhooks.verify_stripe(
      request.body_text,
      request.headers["stripe-signature"],
      env("STRIPE_WEBHOOK_SECRET")
    )
    if (!ok) { return status(401, { error: "bad signature" }) }
    // ... process event
  }
  ```

  Providers shipped today:

  | Function | Provider | Header scheme | Replay protection |
  |---|---|---|---|
  | `webhooks.verify_stripe(payload, sig, secret, tolerance?)` | Stripe | `t=...,v1=...` | yes — 300s default |
  | `webhooks.verify_github(payload, sig, secret)` | GitHub | `sha256=<hex>` | n/a |
  | `webhooks.verify_svix(payload, msg_id, ts, sig, secret)` | Svix (Resend, Clerk, Discord…) | `v1,<base64>` (space-separated) | implicit (header `svix-timestamp`) |
  | `webhooks.verify_shopify(payload, sig, secret)` | Shopify | base64 HMAC-SHA256 | n/a |
  | `webhooks.verify_slack(payload, ts, sig, secret, tolerance?)` | Slack | `v0=<hex>` | yes — 300s default |

- **Tested against published examples.** The GitHub test uses the exact
  signature from GitHub's docs (`Hello, World!` / `It's a Secret to
  Everybody` → `sha256=757107ea0eb2509f...`). The Stripe, Slack, Svix,
  and Shopify tests round-trip the documented signed-string formula
  end-to-end.

- **Stripe + Slack reject stale timestamps** by default (5 minute
  drift window). Pass `tolerance=0` to disable, or any positive
  number of seconds to widen.

- **Constant-time comparison everywhere** (`hmac.Equal`) so signature
  verification is safe against timing attacks.

- **Svix secret format handled.** Secrets stored as `whsec_<base64>`
  are decoded automatically; raw keys also work for users who
  pre-decode.

- **`examples/webhooks.mx`** is a copy-pasteable router with one
  signed handler per provider — drop into any project, set the env
  vars, done.

[0.56.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.56.0

## [0.55.0] — 2026-05-02

### Added — real template engine (if / each / partials)
- **Templates now have control flow.** The mustache-lite engine grew
  conditionals, iteration, and partials — enough to actually build a
  server-rendered site.

  ```
  {{#if logged_in}}<a href="/me">Profile</a>{{else}}<a href="/login">Sign in</a>{{/if}}

  <ul>
    {{#each posts}}
      <li><a href="/p/{{slug}}">{{title}}</a>{{#if featured}} ⭐{{/if}}</li>
    {{/each}}
  </ul>

  {{> header}}
  ```

- **`each` exposes loop context.** Inside the body, `{{this}}` is the
  current item, `{{@index}}` is the 0-based index, and (for object
  iteration) `{{@key}}` is the key. Object items also expose their
  own keys directly so templates can write `{{title}}` instead of
  `{{this.title}}`.

- **Partials.** `render(path, vars, partials)` and
  `render_string(tmpl, vars, partials)` accept an optional third
  argument — a `name -> template-string` object. `{{> name}}` inserts
  the partial in place. Recursion is bounded at depth 16.

- **Same auto-escape default.** `{{ expr }}` is HTML-escaped (defends
  against XSS), `{{{ expr }}}` is raw. Block tags (`{{#if}}`,
  `{{#each}}`, `{{> partial}}`) are not output, only their contents
  per iteration.

- **Truthy rules** match what users expect: empty arrays, empty
  objects, empty strings, zero, false, and null are all falsy.

- **AST-based parser**. Templates parse once into a small node tree,
  then render against a scope stack. Errors point at the unterminated
  block or the missing partial by name.

- **Tests.** Nine new tests cover interpolation+escape, if/else,
  each-of-objects, `@index` / `this`, empty arrays, partials, missing
  partials, unterminated blocks, and nested each-inside-if.

- **`examples/blog.mx`** is a complete server-rendered blog: layout
  with header/footer partials, post list with `{{#each}}`, featured
  badge with `{{#if}}`, and per-post detail pages — about 60 lines.

[0.55.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.55.0

## [0.54.0] — 2026-05-02

### Added — seven new AI providers, one API
- **`ai.complete` and `ai.stream` now speak to ten providers** behind
  the same surface. The four already shipped (OpenAI, Anthropic, Gemini)
  are joined by **xAI Grok, Mistral, DeepSeek, Groq, OpenRouter,
  Together AI, and a local Ollama instance**. Switching providers is
  one string change:

  ```mx
  let summary = ai.complete(text, { provider: "groq",   model: "llama-3.3-70b-versatile" })
  let summary = ai.complete(text, { provider: "deepseek" })
  let summary = ai.complete(text, { provider: "ollama" })   // no API key needed
  ai.stream(prompt, fn(chunk) { write(chunk) }, { provider: "mistral" })
  ```

- **Dispatch table architecture.** Adding the next OpenAI-compatible
  provider is now a single entry in `openAICompatProviders`: name,
  base URL, env-key, default model. The shared `aiCompleteOpenAICompat`
  helper handles the request, parses the standard `choices[0].message`
  envelope, and surfaces clear errors when the env key is missing.

- **Local-first option.** Ollama runs entirely on the developer's
  machine — `provider: "ollama"` posts to `localhost:11434` with no
  API key. Works with any model the user has pulled (`llama3.2`,
  `qwen2.5`, `mistral-small`, …).

- **Tests.** `TestOpenAICompatProvidersTable` validates every entry has
  the right shape and that the seven providers we ship are wired up.
  `TestOpenAICompatRequiresKey` confirms missing env keys produce
  named-after-the-key errors so users know exactly what to set.

- **Example.** `examples/ai_providers.mx` is a copy-pasteable cheat
  sheet with one commented-out line per provider — uncomment, set the
  env var, run.

- **README** now ships a provider matrix table so newcomers can find
  their stack at a glance.

[0.54.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.54.0

## [0.53.0] — 2026-05-02

### Added — VM lowers control flow (2–3× faster on tight loops)
- **`let`, `=`, `if`, `while` are now lowered to bytecode.** The VM
  compiler grew a statement compiler that handles single-binding
  `let`, identifier-target `=`, and `if`/`while` with proper forward
  and backward jumps. Loop bodies and conditional branches run on the
  stack machine instead of the tree-walker.

  ```bash
  $ mx bench /tmp/loop.mx          #  6109 us/op  (tree-walker)
  $ mx bench --bytecode /tmp/loop.mx #  2165 us/op  (bytecode — 2.82× faster)
  ```

  A 1000-iteration arithmetic loop went from 398us/op to 183us/op
  (2.17×). A 10k-iteration loop with `if`/`else` branching went from
  6.1ms/op to 2.2ms/op (2.82×).

- **New opcodes.** `OpStoreVar` (for `let`), `OpAssignVar` (for `=`,
  walks parent scopes), `OpJump` and `OpJumpIfFalse` now use absolute
  PC addressing (cleaner forward-patching).

- **`CompileBlock([]parser.Stmt)` is exported** alongside `CompileExpr`.
  Embedders can now ahead-of-time compile any block of statements that
  fits the supported subset.

- **Statement-level compile cache** (`bcStmtCache`). Whole `if`/`while`
  subtrees compile once per AST node and run as a single program for
  every subsequent execution. Negative results are cached so refusal
  doesn't repeat the compile attempt.

- **Refusal still works correctly.** Destructuring `let`, member-target
  assignments (`a.b = x`), `loop`, `try`, `return`, function calls in
  statement position — all fall back to the tree-walker silently. The
  VM is purely opt-in via `--bytecode` until parity lands.

[0.53.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.53.0

## [0.52.0] — 2026-05-02

### Added — experimental bytecode VM
- **Stack-machine VM behind `--bytecode`.** A new compile-and-run path
  lowers expression statements to a flat instruction stream and executes
  them on a tight `for`/`switch` loop. Constants and identifiers live in
  side tables; arithmetic, comparison, unary, and identifier-load are
  all supported today.

  ```bash
  mx run --bytecode app.mx
  mx bench --bytecode benchmarks/
  ```

  The compiler refuses any node it doesn't fully understand (objects,
  arrays, calls, short-circuit `&&` / `||` / `??`, control flow) and
  falls back to the tree-walker transparently — so semantics are
  preserved on programs the VM only partially covers.

- **Per-interpreter compile cache.** Each AST expression is compiled at
  most once; subsequent visits hit `Compiled` directly. Negative results
  are also cached so the compiler doesn't re-attempt nodes it already
  refused.

- **Public API.** `interpreter.CompileExpr(parser.Expr) (*Compiled, bool)`
  and `(*Compiled).Run(*Env)` are exported so embedders, the LSP, or
  future JIT experiments can target the same bytecode format. The
  interpreter exposes `SetBytecode(bool)` and `BytecodeEnabled()`.

- **Test coverage for the VM.** `interpreter/vm_test.go` exercises
  arithmetic, comparison, identifier load, undefined-identifier errors,
  divide-by-zero, refusal of short-circuit operators, refusal of
  unsupported nodes, and an end-to-end parity check that runs the same
  program through both engines and compares results.

The VM is opt-in and off by default. It will become the default once it
has parity with the tree-walker for every node type. Today it's a
faster path for tight numeric / data-shuffling expression statements,
which is the foundation for the upcoming function-body lowering.

[0.52.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.52.0

## [0.51.0] — 2026-05-02

### Distribution
- **VS Code extension `.vsix` is now a release asset.** GoReleaser
  runs `npx @vscode/vsce package` in the pre-build hook and attaches
  the resulting `mxscript-<version>.vsix` to every tag. Users can
  one-line install:

  ```bash
  curl -fsSL -o mx.vsix \
    https://github.com/jlkdevelop/mxscript/releases/latest/download/mxscript-0.51.0.vsix
  code --install-extension mx.vsix
  ```

  The hook gracefully no-ops when Node / npx isn't available locally.

### Adoption
- **github-linguist PR scaffold polished.** `extras/linguist/` now
  ships a ready-to-apply `languages.yml.patch`, the brand-color hex
  to add (`#2B54A8` from the logo), and a step-by-step PR procedure
  for when MX hits the public-adoption threshold.

[0.51.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.51.0

## [0.50.0] — 2026-05-02

### Added — LSP polish (v1.0 of the editor experience)
- **Signature help** — when your cursor is inside a function call, the
  editor now shows the call signature with the active parameter
  highlighted. Counts commas at the cursor's nesting depth so it works
  through nested calls.

  ```
  json_stringify(v, pretty?) -> string
                    ^^^^^^^^ (active)
  ```

- **Snippet completions** — 16 curated snippets covering common
  patterns: `route`, `post`, `group`, `mw`, `ws`, `sse`, `fn`, `match`,
  `try`, `spawn`, `test`, `bench`, `server`, `sql.migrate`, `session`,
  `openapi`. Each has tab-stops (`$1`, `$2`, …) so you can fill in
  blanks fluidly.

  Type `route` + Tab and you get a complete `get /path { return json({}) }`
  block with the path pre-selected for editing.

[0.50.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.50.0

## [0.49.0] — 2026-05-02

### Added
- **Redis namespace** — pure-Go go-redis/v9 client.

  ```mx
  let r = redis.connect("redis://localhost:6379/0")

  redis.set(r, "user:1", "Jassim", { ttl_seconds: 3600 })
  print(redis.get(r, "user:1"))
  print(redis.incr(r, "page-views"))
  redis.publish(r, "events", json_stringify({ kind: "login" }))
  redis.del(r, "user:1")
  redis.close(r)
  ```

  Surfaced ops: `connect`, `set` (with optional `ttl_seconds`), `get`,
  `del` (variadic), `incr`, `expire`, `publish`, `close`.

- **MySQL support** — `sql.open` now auto-routes by DSN:
  - `./local.db` / `:memory:` → SQLite
  - `postgres://...` / `postgresql://...` → Postgres (lib/pq)
  - `mysql://user:pass@host/db` or `user:pass@tcp(host:port)/db` → MySQL

  All existing `sql.exec / query / query_one / transaction / migrate`
  helpers work transparently across all three.

[0.49.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.49.0

## [0.48.0] — 2026-05-02

### Added
- **Argon2id + scrypt password hashing**:
  - `password.hash_argon2(plain)` / `password.verify_argon2(plain, stored)`
    — OWASP-recommended defaults (m=64MiB, t=3, p=4).
  - `password.hash_scrypt(plain)` / `password.verify_scrypt(plain, stored)`
    — N=32768, r=8, p=1.
  Both store self-describing strings (`$argon2id$...`, `$scrypt$...`)
  so verification is portable to any compliant implementation.
- **YAML**: `yaml_parse(s)` / `yaml_stringify(v)` (gopkg.in/yaml.v3).
- **TOML**: `toml_parse(s)` / `toml_stringify(v)` (BurntSushi/toml).

  ```mx
  let cfg = yaml_parse(read_file("./config.yml"))
  let toml_text = toml_stringify({ name: "MX", version: "1.0" })
  ```

### Dependencies
Three new deps to support the above: `golang.org/x/crypto` (argon2,
scrypt), `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml`. All three
are widely-deployed and pure-Go.

[0.48.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.48.0

## [0.47.0] — 2026-05-02

### Added — AI vision + similarity
- **`ai.vision(prompt, images, opts?)`** — multimodal completion. Each
  image is a URL or `data:` URL. Default model `gpt-4o-mini`.

  ```mx
  let r = ai.vision("Describe this", [
    "https://example.com/photo.jpg",
    "data:image/jpeg;base64," + base64_encode(read_file("./local.jpg"))
  ])
  ```

- **`ai.similarity(a, b)`** — cosine similarity between two embedding
  vectors. Returns a number in `[-1, 1]`. Useful for semantic search,
  clustering, dedup:

  ```mx
  let q = ai.embed("cute pet animals")
  loop docs as d {
    let score = ai.similarity(q, d.embedding)
    if (score > 0.85) { print(d.title) }
  }
  ```

[0.47.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.47.0

## [0.46.0] — 2026-05-02

### Added
- **CSRF helpers**:
  - `csrf_token(secret, session_id)` — deterministic token bound to
    a session. Embed in forms / app state.
  - `verify_csrf(secret, session_id, token)` — constant-time check.

  ```mx
  post /transfer {
    let sid = request.cookies?.sid ?? "anon"
    if (!verify_csrf(env("CSRF_SECRET"), sid, request.body._csrf)) {
      return status(403, { error: "csrf" })
    }
    ...
  }
  ```

- **Pub/sub** for in-process broadcast — perfect for WebSocket
  fan-out without maintaining a per-route registry:

  ```mx
  let chat = pubsub.topic()

  ws /chat {
    let sub = chat.subscribe(send)
    while (true) {
      let m = recv()
      if (m == null) { break }
      chat.publish(m)              // broadcast to every connected ws
    }
    sub.unsubscribe()
  }
  ```

  `pubsub.topic()` returns an object with `subscribe(fn)`, `publish(value)`,
  and `count()`. Subscribe returns a handle with `unsubscribe()`.
  Subscriber errors are caught so a single bad listener can't take
  the topic down.

[0.46.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.46.0

## [0.45.0] — 2026-05-02

### Documentation
- **README hero refresh** — the example up top now uses shorthand
  routes, route groups, validate, and sql.migrate, ending with an
  auto-generated Swagger UI. Reflects what the language actually
  feels like at v0.44+.
- **README "What ships in the box" matrix** — quick scan of the
  practical surface across web framework / real-time / database /
  auth / AI / background jobs / API tooling / stdlib / concurrency
  / tooling / editor / distribution.
- **`examples/full_app.mx`** — kitchen-sink showcase. ~150 lines
  exercising sql.migrate, validate, password.hash + verify, JWT
  signup / login, route groups + middleware, SSE feed, AI summarise,
  OpenAPI + Swagger UI + status_page.

[0.45.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.45.0

## [0.44.0] — 2026-05-02

### Performance
- **Route lookup is now O(segments)**, not O(routes). New
  `route_trie.go` builds a path-segment trie on first request:
  static segments live in a per-node map (O(1) hit), `:param`
  children take a single slot, and the trie is reused across all
  subsequent dispatches. Static-vs-`:param` precedence and the
  SSE/WS-as-GET behavior are preserved.

  For apps with hundreds of routes this is the difference between a
  full linear scan per request and a constant-time descent.

### Internal
- Existing `matchPath` helper is unchanged but no longer on the hot
  path; the trie's `matchSegs` is now the dispatcher.
- Trie is rebuilt opportunistically — first request after registration
  pays the cost once, then everything after that is cached.

[0.44.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.44.0

## [0.43.0] — 2026-05-02

### Added
- **`mx upgrade`** — self-update. Hits the GitHub releases API, picks
  the asset for your OS/arch (the GoReleaser `mx_<ver>_<os>_<arch>`
  name pattern), extracts the binary from the .tar.gz / .zip, and
  swaps it in place atomically. `--force` re-installs even when
  you're already on the latest.
- **`mx doctor`** — env / install / network diagnostic. Prints version,
  binary path, platform + Go runtime, common env vars (with redacted
  values), and a quick reachability check against GitHub & OpenAI.

  ```
  $ mx doctor
  MX Script doctor
    version:    v0.43.0
    binary:     /usr/local/bin/mx
    platform:   darwin/arm64 (Go go1.25.0)

  env:
    OPENAI_API_KEY:    set (51 chars)
    ANTHROPIC_API_KEY: —
    JWT_SECRET:        set (32 chars)
    ...

  network:
    ✓ GitHub releases    230ms
    ✓ OpenAI              74ms
  ```

[0.43.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.43.0

## [0.42.0] — 2026-05-02

### Added
- **Namespaced imports** — `import "./auth.mx" as auth` exposes the
  module's top-level `let` / `fn` declarations as members of an
  `auth` object. Module-internal state stays encapsulated:

  ```mx
  // auth.mx
  let secret = "internal-only"
  fn make_token(user_id) { return jwt.sign({ sub: user_id }, secret) }
  fn verify(token)       { return jwt.verify(token, secret) }
  ```

  ```mx
  // app.mx
  import "./auth.mx" as auth

  let t = auth.make_token("jassim")  // works
  let c = auth.verify(t)              // works
  // print(secret)                    // error: undefined — encapsulated
  ```

  The flat form (`import "./utils.mx"` with no `as`) still works and
  dumps every top-level binding into the importing scope.

[0.42.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.42.0

## [0.41.0] — 2026-05-02

### Added
- **`mx new <template> [name]`** — opinionated project starters. Each
  template scaffolds a complete, runnable app (one .mx file plus
  README, .env.example, .gitignore):

  ```
  mx new api my-api      # REST API + OpenAPI + Swagger UI + status page
  mx new todo my-todos   # Auth + JWT + SQLite + groups + validate
  mx new chat realtime   # WebSockets + sessions + broadcast
  mx new ai bot          # Tool-calling agent with 5-turn loop
  mx new blog blog       # SSR blog with markdown posts + admin
  ```

  Each template is a complete reference implementation showing how
  the relevant subset of MX fits together — `mx new` itself is the
  best way to learn the language.

[0.41.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.41.0

## [0.40.0] — 2026-05-02

### Added — durable background jobs
- **`jobs` namespace** — SQLite-backed job queue. Jobs survive
  restarts; workers retry failures with exponential backoff
  (5s → 10s → 20s …) until `max_attempts` is reached.

  ```mx
  let q = jobs.create({
    db: "./jobs.db",
    queue: "emails",
    max_attempts: 3
  })

  // Producer
  q.enqueue({ to: "alice@example.com", subject: "Hi" })
  q.enqueue({ to: "bob@example.com", subject: "Hi" }, { delay_seconds: 60 })

  // Consumer — N concurrent workers
  let stop = q.process(2, fn(job) {
    email.send({ to: job.to, subject: job.subject, ... })
  })

  // Inspection
  print(q.stats())   // { pending, running, done, failed }
  ```

  The underlying `mx_jobs` table is created on first call. Schema:
  `id`, `queue`, `payload`, `status` (pending|running|done|failed),
  `attempts`, `last_error`, `run_at`, `created_at`. WAL mode +
  busy-timeout + an in-process mutex on the claim path keep
  concurrent workers safe.

[0.40.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.40.0

## [0.39.0] — 2026-05-02

### Added
- **Route groups** — `group /prefix { ... }` nests routes under a
  shared path prefix and shared middlewares:

  ```mx
  group /api/v1 {
    use require_auth          // runs on every nested route
    get /users           { return json(users) }
    get /users/:id       { return json(find_user(request.params.id)) }
    post /users          { return status(201, request.body) }
  }

  group /api/v2 {
    get /users           { return json(users_v2) }
  }
  ```

  Groups can nest. `use` statements at the top of a group attach to
  every route in scope; siblings outside the group are unaffected.
  Path joining handles trailing slashes (`/api/v1` + `/users` →
  `/api/v1/users`).

[0.39.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.39.0

## [0.38.0] — 2026-05-02

### Added
- **`validate(value, schema)`** — JSON-Schema-lite input validation.
  Returns `{ valid, errors: [{ path, message }, ...] }`. Drop it
  into a route to reject malformed bodies in two lines:

  ```mx
  let user_schema = {
    type: "object",
    properties: {
      name:  { type: "string", min_length: 2, max_length: 50 },
      age:   { type: "integer", minimum: 0, maximum: 150 },
      email: { type: "string", format: "email" },
      role:  { type: "string", enum: ["admin", "user", "guest"] },
      tags:  { type: "array", items: { type: "string" } }
    },
    required: ["name", "email"]
  }

  post /users {
    let r = validate(request.body, user_schema)
    if (!r.valid) { return status(400, { errors: r.errors }) }
    // ... save the user
  }
  ```

  Supported keys: `type` (string / number / integer / bool / array /
  object / any), `enum`, `required`, `properties`, `items`,
  `minimum` / `maximum`, `min_length` / `max_length`, `min_items` /
  `max_items`, `pattern` (regex), `format` (`email`, `url`, `uuid`,
  `date`).

[0.38.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.38.0

## [0.37.0] — 2026-05-02

### Added — AI tool calling
- **`ai.complete(prompt, { tools: [...] })`** now supports OpenAI-style
  function / tool calling. When tools are passed (or a `messages` array
  for multi-turn chats), the response is structured:

  - **Text answer**: returns a plain string (existing behavior).
  - **Tool call**: returns `{ tool_calls: [{ id, name, arguments }, ...], content? }`.
    `arguments` is a fully parsed object — no need to `json_parse` it.

  ```mx
  let tools = [
    {
      name: "get_weather",
      description: "Get the current temperature for a city",
      params: {
        type: "object",
        properties: { city: { type: "string" } },
        required: ["city"]
      }
    }
  ]
  let r = ai.complete("What's the weather in Paris?", { tools: tools })
  if (r.tool_calls != null) {
    // call your function, feed the result back via messages: [...]
  }
  ```
- **Multi-turn chat support**: pass `messages: [{role, content, ...}, ...]`
  to keep an ongoing conversation across calls. Required for the agent
  loop pattern (call → tool result → call → answer).

### Added — example
- **`examples/agent.mx`** — 60-line tool-calling agent. Three tools
  (`get_time`, `calc`, `fetch_url`), a 5-turn loop, and a final answer.
  Run with `OPENAI_API_KEY=... mx run examples/agent.mx`.

[0.37.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.37.0

## [0.36.0] — 2026-05-02

### Added
- **PostgreSQL support** — `sql.open` now picks the driver from the
  DSN shape:
  - `"./local.db"` / `":memory:"` → SQLite (modernc.org/sqlite)
  - `"postgres://..."` / `"postgresql://..."` → Postgres (`lib/pq`)

  ```mx
  let db = sql.open(env_required("DATABASE_URL"))
  sql.exec(db, "CREATE TABLE IF NOT EXISTS users (id SERIAL PRIMARY KEY, name TEXT)")
  let r = sql.exec(db, "INSERT INTO users (name) VALUES ($1) RETURNING id", "Jassim")
  ```

  All existing helpers (`exec` / `query` / `query_one` / `transaction`
  / `migrate`) work transparently against both backends.
- **`status_page(opts?)`** — drop-in HTML status dashboard. Renders
  uptime, route count, static-mount count, middleware count, plus a
  color-coded route table.

  ```mx
  get /status { return status_page({ app: "My API" }) }
  ```

[0.36.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.36.0

## [0.35.0] — 2026-05-02

### Added
- **`mx bench [path]`** — built-in benchmark runner. Discovers
  `*_bench.mx` files; runs every `bench_*` function with auto-scaled
  iterations targeting a 1-second wall-clock budget per benchmark.

  ```mx
  fn bench_json_stringify() {
    json_stringify({ id: 1, name: "Jassim", scores: [10,20,30] })
  }
  fn bench_hash_sha256() {
    hash_sha256("the quick brown fox")
  }
  ```

  ```
  $ mx bench
    json stringify       629,073 ops    1.94 us/op    (516,210 ops/s)
    hash sha256        4,063,116 ops    0.29 us/op  (3,455,304 ops/s)
  ```

- **`fs.watch(path, fn, opts?)`** — recursive directory polling
  watcher. Calls `fn(event)` for every file change with
  `{ kind: "added"|"modified"|"removed", path }`. Pure stdlib (no
  fsnotify dep). Returns a stop function. Default 500 ms poll
  interval, configurable via `opts.interval_ms`.

[0.35.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.35.0

## [0.34.0] — 2026-05-02

### Added
- **Interactive API docs out of the box**:

  ```mx
  get /openapi.json { return json(openapi({ title: "My API" })) }
  get /docs        { return swagger_ui("/openapi.json") }   # Swagger UI
  get /reference   { return redoc_ui("/openapi.json") }     # Redoc
  ```

  `swagger_ui(spec_url, opts?)` and `redoc_ui(spec_url, opts?)` return
  ready-to-serve HTML pages that load the spec from the URL you pass.
  No npm, no bundling — both pull from CDN.

- **`sql.migrate(db, migrations)`** — schema versioning, idempotent.
  Migrations is an ordered array of SQL strings. Each gets a hash
  recorded in a `mx_migrations` bookkeeping table; reruns skip
  already-applied migrations and refuse to run if a migration's
  text has been edited since it was applied (corruption guard).

  ```mx
  sql.migrate(db, [
    "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
    "ALTER TABLE users ADD COLUMN email TEXT",
    "CREATE INDEX users_name_idx ON users(name)"
  ])
  // returns { applied: [<n>, ...], skipped: [<m>, ...] }
  ```

[0.34.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.34.0

## [0.33.0] — 2026-05-02

### Added
- **Gemini AI provider**:

  ```mx
  let answer = ai.complete("hello", {
    provider: "gemini",
    model: "gemini-2.0-flash",
    max_tokens: 200
  })
  ```

  Reads `GEMINI_API_KEY` (or `GOOGLE_API_KEY`). The default model is
  `gemini-2.0-flash` when `provider="gemini"`. Three providers now
  ship in stdlib: OpenAI (default), Anthropic, Gemini.

- **`sql.transaction(db, fn)`** — runs `fn(tx)` inside a transaction.
  If the function throws, the transaction is rolled back and the
  error is re-raised. If it returns normally, the transaction is
  committed and the return value is propagated to the caller.

  ```mx
  sql.transaction(db, fn(tx) {
    sql.exec(tx, "UPDATE accounts SET balance = balance - 50 WHERE id = ?", 1)
    sql.exec(tx, "UPDATE accounts SET balance = balance + 50 WHERE id = ?", 2)
  })
  ```

  All `sql.exec` / `sql.query` / `sql.query_one` calls inside `fn`
  use the transaction handle automatically — same API, just the
  pooled-vs-transaction routing happens behind the scenes.

[0.33.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.33.0

## [0.32.0] — 2026-05-02

### Added
- **`openapi(info?)`** — generates a valid OpenAPI 3.1 spec from your
  declared routes. Path params are automatically converted from MX's
  `:id` syntax to OpenAPI's `{id}` and emitted as parameter declarations.

  ```mx
  get /api/users/:id { return json({}) }
  post /api/users    { return json({}, ...) }

  get /api/openapi.json {
    return json(openapi({
      title: "My API",
      version: "1.0.0",
      description: "Auto-introspected by MX Script."
    }))
  }
  ```

  Drop the URL into Swagger UI / Stoplight Elements / Redoc and you
  have an instant interactive API browser.
- **`routes()`** — companion helper that returns an array of
  `{ method, path }` for every registered route. Handy for status
  pages or admin dashboards.

[0.32.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.32.0

## [0.31.0] — 2026-05-02

### Added
- **`mx test --cover`** — line coverage report. Runs your `*_test.mx`
  suite as usual, then prints `covered / total lines (%)` per file:

  ```
  examples/stdlib_test.mx
    ✓ strings
    ✓ arrays
    ...
    coverage: 67/67 lines (100.0%)
  ```

  Implementation:
  - `Interpreter.EnableCoverage()` flips on a per-statement line-hit
    recorder. Off by default — non-test runs pay zero overhead.
  - `parser.ExecutableLines(prog)` walks the AST to compute the set
    of lines that *could* run (statements only — comments, blank
    lines, and pure expressions don't count).
  - The reporter diffs the two and renders the percentage.

[0.31.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.31.0

## [0.30.0] — 2026-05-02

### Added — sessions
- **`session` namespace** wires JWT-style claims to a signed cookie:

  ```mx
  post /login {
    return session.create({ user_id: 42, role: "admin" }, {
      secret: env_required("SESSION_SECRET"),
      max_age: 86400
    })
  }

  get /me {
    let claims = session.read(request, env("SESSION_SECRET"))
    if (claims == null) { return status(401) }
    return json(claims)
  }

  post /logout { return session.clear() }
  ```

  - `session.create(claims, opts)` returns a Response that sets the
    cookie. `opts` accepts `secret` (required), `max_age`, `name`,
    `path`, `domain`, `same_site`, `http_only`, `secure`, `body`.
  - `session.read(request, secret, name?)` returns the claims object
    or `null` if the cookie is missing / tampered.
  - `session.clear(opts?)` returns a Response that expires the cookie.

### Added — example
- **`examples/chat.mx`** — an 80-line real-time chat app using
  WebSockets + sessions + a built-in single-page HTML client.

[0.30.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.30.0

## [0.29.0] — 2026-05-02

### Added — LSP intelligence
- **`textDocument/hover`** now returns a real signature + summary for
  every built-in function and keyword. Position your cursor over
  `json_stringify` and the editor shows:

      json_stringify(v, pretty?) -> string

      Serialize to JSON.
- **`textDocument/completion`** offers all ~110 built-ins plus the
  ~40 language keywords, each with a one-line `detail` and a markdown
  documentation block. The editor handles prefix filtering client-side.
- **Curated `builtinDocs` registry** in `lsp/lsp.go` — single source
  of truth for hover / completion docs. Adding a new built-in to the
  language now means adding one entry here too.

[0.29.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.29.0

## [0.28.0] — 2026-05-02

### Added — destructuring polish
- **Renaming** in object destructure: `let { name: n, role: r } = user`.
- **Default values** when the source key is missing or null:
  `let { name = "anon" } = user`. Works on both object and array forms.
- **Rest in array destructure**: `let [head, ...tail] = arr`. The
  rest binding gathers the remaining elements as an array. Must be
  last in the pattern.
- All three combine: `let { name: n = "anon" } = user`.

[0.28.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.28.0

## [0.27.0] — 2026-05-02

### Added
- **Destructuring assignment** in `let`:

  ```mx
  let { name, role } = user        # object: bind keys
  let [a, b, c] = arr              # array: bind positions
  ```

  Missing keys / out-of-range indexes bind to `null`. Renaming
  (`{ name: n }`) is on the roadmap for v0.28.
- **Humanized time**: `time_ago(unix_ms)` returns strings like
  `"5 minutes ago"`, `"2 days ago"`, `"in 30 seconds"`, `"just now"`.
  `time_human(unix_ms)` returns `"Sat May 2 2026 15:41"` in the
  local time zone.

[0.27.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.27.0

## [0.26.0] — 2026-05-02

### Added
- **Password hashing** via `password.hash` / `password.verify`. Uses
  PBKDF2-SHA256 with 100k iterations and a random per-password salt.
  Stored format is the standard `pbkdf2-sha256$<iter>$<salt>$<hash>`,
  so verification is portable. Implemented in stdlib (no x/crypto
  dependency).

  ```mx
  let stored = password.hash("hunter2")            // store this in DB
  if (password.verify(input, stored)) { ... }      // login check
  ```
- **AES-256-GCM encryption** for at-rest secrets. The key can be a
  full 32-byte string or any passphrase (auto-derived via SHA-256).
  Output is base64( nonce || ciphertext || auth tag ).

  ```mx
  let cipher = aes_encrypt("ssn:123-45-6789", env("ENC_KEY"))
  let plain  = aes_decrypt(cipher, env("ENC_KEY"))
  ```
- **`ai.stream(prompt, on_chunk, opts?)`** — streaming LLM responses.
  `on_chunk` is called once per token-delta with the new piece of
  text; the final return value is the full concatenated response.
  Lets you echo tokens to the client as they arrive (combine with
  the `sse` route type for a typewriter-style UI).

[0.26.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.26.0

## [0.25.0] — 2026-05-02

### Added
- **`shell(cmd, args?, opts?)`** runs an OS command and returns
  `{ stdout, stderr, exit_code }`. Useful for build glue, devops
  scripts, and anything that previously meant dropping to bash:

  ```mx
  let r = shell("git", ["log", "-1", "--format=%H"])
  if (r.exit_code != 0) { error(r.stderr) }
  print("HEAD:", trim(r.stdout))
  ```

  `opts` may include `dir`, `env`, `stdin`, `timeout_ms`.
- **CSV**: `csv_parse(text)` returns an array of arrays;
  `csv_stringify(rows)` is the inverse. Tolerates ragged rows.
- **`format(fmtStr, ...args)`** — printf-style. `%s`, `%d`, `%f`, `%v`.
- **"Did you mean ..." suggestions** on undefined-identifier errors.
  The interpreter walks every name in scope and proposes the closest
  match within Levenshtein distance 2. Catches the obvious typos like
  `prnt` → `print`, `jsonn_stringify` → `json_stringify`.

[0.25.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.25.0

## [0.24.0] — 2026-05-02

### Added
- **Language Server** via `mx lsp`. JSON-RPC 2.0 over stdio,
  implementing the editor essentials:
  - `initialize` / `shutdown` / `exit` lifecycle
  - `textDocument/didOpen` / `didChange` / `didClose` buffer tracking
  - `textDocument/publishDiagnostics` — parse errors reported as
    inline squiggles
  - `textDocument/formatting` — runs `mx fmt` on the whole document
  - `textDocument/hover` (stub for now; wired up so clients don't err)

  Configure any LSP-capable editor:

  ```jsonc
  // VS Code, Helix, Neovim, Zed, Sublime LSP — all the same shape.
  {
    "command": "mx",
    "args": ["lsp"],
    "filetypes": ["mxscript"]
  }
  ```

  The bundled VS Code extension now declares the LSP integration in
  `package.json`; the next extension publish will wire it through
  automatically.

[0.24.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.24.0

## [0.23.0] — 2026-05-02

### Added
- **Docs site** at <https://mxscript.com> — landing page + full
  reference. Built as a static site under `site/` and deployed on
  every push to `main` via the new `pages.yml` GitHub Actions
  workflow. Light + dark theme, mobile-friendly, no client-side
  framework — just hand-written HTML and CSS.
  Assets:
  - `site/index.html` — landing page (hero, demo block, feature grid, install)
  - `site/docs.html` — single-page docs with sticky sidebar
  - `site/style.css` + `site/docs.css` — design system using brand
    colors (`#2B54A8` blue, `#FDC02E` yellow)
  - `site/CNAME` — `mxscript.com` (point your DNS to GitHub Pages
    once the workflow has run once)

### Notes
DNS: in Cloudflare / your registrar, add a CNAME record
`mxscript.com` → `jlkdevelop.github.io`. After the first Pages
deploy succeeds, GitHub will provision the TLS certificate
automatically.

[0.23.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.23.0

## [0.22.0] — 2026-05-02

### Added — SQL
- **`sql` namespace** for SQLite via the pure-Go `modernc.org/sqlite`
  driver (no CGo required). The first external Go dependency MX has
  added — added because real apps need real persistence and KV alone
  doesn't cut it. CONTRIBUTING.md now documents the exception.

  ```mx
  let db = sql.open("./data.db")

  sql.exec(db, "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT)")
  let r = sql.exec(db, "INSERT INTO users (name) VALUES (?)", "Jassim")
  print("inserted id:", r.last_insert_id)

  let users = sql.query(db, "SELECT * FROM users WHERE name LIKE ?", "Jass%")
  loop users as u { print(u.id, u.name) }

  let one = sql.query_one(db, "SELECT * FROM users WHERE id = ?", 1)
  if (one == null) { /* not found */ }

  sql.close(db)
  ```
- **`KindHandle` value kind** — opaque resource carrier. SQLite uses it
  today; future helpers (Postgres, file streams) can reuse the shape.

### Changed
- Go module bumped to 1.22 minimum, toolchain auto-fetches 1.25.0+ as
  required by the new SQLite dep. CI builds against 1.25.

[0.22.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.22.0

## [0.21.0] — 2026-05-02

### Added — WebSockets
- **`ws /path { ... }`** is the third route flavour after `route` and
  `sse`. RFC 6455 implemented in pure stdlib — no external dependency.

  ```mx
  ws /chat {
    while (true) {
      let msg = recv()              // null on peer-close
      if (msg == null) { break }
      send("echo: " + msg)
    }
  }
  ```

  Three functions are injected into the route scope:
  - `recv()` — block until the next message; returns the payload as a
    string (or `null` if the peer closed).
  - `send(value)` — send a text frame. Strings go through verbatim;
    other values get JSON-encoded automatically.
  - `close(code?, reason?)` — explicit close (default code 1000).

  The handler transparently handles ping/pong, fragmented messages,
  and the closing handshake. Hard 16 MiB cap per message to bound
  memory.

[0.21.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.21.0

## [0.20.0] — 2026-05-02

### Added
- **`mx fmt`** — opinionated formatter for `.mx` files.

  ```bash
  mx fmt path/to/file.mx           # print to stdout
  mx fmt -w path/to/file.mx        # rewrite in place
  mx fmt --check path/to/file.mx   # exit 1 if anything would change
  mx fmt examples/                 # recursive on a directory
  cat foo.mx | mx fmt              # stdin → stdout
  ```

  Conventions:
  - 2-space indent
  - Standard operator spacing (`a + b`, `a == b`, `a && b`)
  - `{` stays on the line it opens; block contents indent on the next
    line; `}` on its own line
  - Object literals stay tight on a single line when short
  - Route paths (`get /users/:id`) keep tight spacing
  - Comments and intentional blank lines are preserved
- **Lexer's `CollectComments` flag** preserves `//`, `#`, `/* */` as
  `TokenComment` and line breaks as `TokenNewline`. The parser still
  ignores them; tooling (formatter, future LSP) can opt in.

[0.20.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.20.0

## [0.19.0] — 2026-05-02

### Added
- **Image namespace** (PNG / JPEG via Go's stdlib `image`):

  ```mx
  post /upload {
    let f = request.files?.image
    let info = image.info(f.content)         // { format, width, height }

    // Resize for the avatar slot
    let avatar = image.resize(f.content, 256, 256, { format: "jpeg", quality: 80 })
    write_file("./uploads/${uuid()}.jpg", avatar)

    // Always store a PNG copy
    let archive = image.convert(f.content, "png")
    write_file("./archive/${uuid()}.png", archive)

    return json({ ok: true, original: info })
  }
  ```

  - `image.info(bytes)` — `{ format, width, height }`
  - `image.resize(bytes, w, h, opts?)` — nearest-neighbour resize
    (good enough for thumbnails). `opts` may include `format`
    (`"png"` / `"jpeg"`) and `quality` (JPEG, 0-100).
  - `image.convert(bytes, format, quality?)` — re-encode without
    resizing.
  - GIF input is decoded but not encoded back (use png / jpeg).

[0.19.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.19.0

## [0.18.0] — 2026-05-02

### Added
- **OAuth2 helpers** for the most common providers — Google, GitHub,
  Discord, LinkedIn, Microsoft. Two functions cover the whole flow:

  ```mx
  // 1. Send the user to the provider's consent page
  get /auth/google {
    return redirect(oauth.authorize_url({
      provider: "google",
      client_id: env_required("GOOGLE_CLIENT_ID"),
      redirect_uri: "https://app.example.com/auth/callback",
      scopes: ["openid", "email", "profile"],
      state: uuid()
    }))
  }

  // 2. Exchange ?code= for tokens on the callback
  get /auth/callback {
    let tokens = oauth.exchange_code({
      provider: "google",
      client_id: env_required("GOOGLE_CLIENT_ID"),
      client_secret: env_required("GOOGLE_CLIENT_SECRET"),
      redirect_uri: "https://app.example.com/auth/callback",
      code: request.query.code
    })
    // tokens.access_token, tokens.id_token, ...
    return json(tokens)
  }
  ```

  Custom providers: pass `authorize_url` and `token_url` instead of
  `provider`. The token-exchange response is decoded as JSON, falling
  back to form-urlencoded for older providers (GitHub).

[0.18.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.18.0

## [0.17.0] — 2026-05-02

### Added
- **gzip compression**: `server { compression: true }` adds a transparent
  gzip middleware. Clients sending `Accept-Encoding: gzip` get a
  ~10x-smaller response; everyone else gets the unencoded body.
- **`markdown(s)`**: render a small subset of CommonMark to safe HTML —
  headings, paragraphs, bold, italic, inline code, links, ordered &
  unordered lists, fenced code blocks. All input is HTML-escaped first
  so it's safe for untrusted text.
- **Anthropic Claude provider** for `ai.complete`:

  ```mx
  let answer = ai.complete("In one sentence, what is MX Script?", {
    provider: "anthropic",
    model: "claude-haiku-4-5-20251001",
    max_tokens: 200
  })
  ```

  Reads `ANTHROPIC_API_KEY` from the env. Default OpenAI provider
  unchanged.

[0.17.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.17.0

## [0.16.0] — 2026-05-02

### Added — Concurrency
- **`spawn { ... }`** runs a block in a fresh goroutine. The body shares
  the enclosing scope (Env is now RWMutex-protected) but the convention
  is: use channels for coordination, don't mutate shared state.

  ```mx
  let ch = chan(10)
  spawn {
    loop 5 as i { send(ch, i * 2) }
    close_chan(ch)
  }
  while (true) {
    let v = recv(ch)
    if (v == null) { break }   // null means closed
    print(v)
  }
  ```
- **Channels**: `chan(capacity?)` allocates a buffered (or unbuffered)
  channel. `send(ch, value)` puts; `recv(ch)` takes (returns null on
  closed); `close_chan(ch)` closes (idempotent).
- **`wait_group()`** wraps `sync.WaitGroup` for the classic "fork N
  goroutines, wait for all" pattern:

  ```mx
  let wg = wait_group()
  loop urls as u {
    wg.add(1)
    spawn { fetch(u); wg.done() }
  }
  wg.wait()
  ```

[0.16.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.16.0

## [0.15.0] — 2026-05-02

### Added
- **Email** via stdlib `net/smtp`:

  ```mx
  email.send({
    host: env_required("SMTP_HOST"),
    port: 587,
    username: env("SMTP_USER"),
    password: env("SMTP_PASS"),
    from: "noreply@mxscript.com",
    to: "user@example.com",
    subject: "Welcome",
    body: "<p>Thanks for signing up!</p>",
    html: true
  })
  ```
- **Rate limiting** as a server config option. Per-IP token bucket
  refills linearly:

  ```mx
  server {
    rate_limit: { requests: 60, per: "1m" }
  }
  ```

  Excess requests get a 429 with a `Retry-After` header.
- **Webhook signature verification** — `verify_webhook(secret, body,
  signature, scheme?)` supports `"hex"` (default), `"base64"`,
  `"github"` (`sha256=<hex>`), and `"stripe"` (`t=<ts>,v1=<hex>`).

  ```mx
  post /webhook {
    let sig = request.headers["x-hub-signature-256"]
    if (!verify_webhook(env("WH_SECRET"), request.body, sig, "github")) {
      return status(401, { error: "bad signature" })
    }
    // ...handle event
  }
  ```

[0.15.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.15.0

## [0.14.0] — 2026-05-02

### Added
- **Templates**: `render(path, vars?)` and `render_string(tmpl, vars?)`.
  Uses `{{ var.path }}` placeholders (HTML-escaped by default for XSS
  safety) or `{{{ var.path }}}` for raw passthrough. Triple-brace is the
  escape hatch when you really want unescaped HTML in.

  ```mx
  get / {
    return render("./views/index.html", { user: { name: "Jassim" } })
  }
  ```
- **Structured logger**: `log.info`, `log.warn`, `log.error`, `log.debug`
  emit RFC 3339 UTC timestamps with colored level tags to stderr.
- **Date arithmetic**: `add_days`, `add_hours`, `add_minutes`,
  `days_between`, `weekday`. Operate on Unix milliseconds (the same
  shape `now()` and `parse_date` return).
- **Request convenience**: `request.bearer_token` (auto-stripped from
  `Authorization: Bearer ...`), `request.is_json` (boolean), and
  `request.ip` (honors `X-Forwarded-For` and `X-Real-IP`).
- **Vercel adapter** (`mx build --vercel`): generate a deployable Go project
  from any `.mx` app. Vercel's Go framework preset auto-detects the output
  and runs it on the platform-provided `$PORT`.
  ```bash
  mx build --vercel app.mx
  git add main.go go.mod vercel.json
  git commit -m "Deploy via mx build --vercel"
  git push   # Vercel autodeploys
  ```
- **Public embedder API on `Interpreter`**:
  - `Load(prog *parser.Program) error` — evaluates the program and registers
    routes/middleware/server config without starting a listener
  - `Handler() http.Handler` — returns the fully-wrapped HTTP handler
    (mux + CORS + logging + max-body)
  - `HasRoutes() bool` — reports whether any routes or static mounts exist
  These are the building blocks that any host (Vercel, Fly, Cloudflare,
  in-process tests) can use to mount an MX app inside its own server.

[0.14.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.14.0

## [0.13.0] — 2026-05-02

### Added
- **Multipart file uploads**: `request.files` is auto-populated when the
  request body is `multipart/form-data`. Each file is an object with
  `name`, `size`, `content_type`, and `content` (raw bytes as string).
  Plain form fields stay in `request.body` as a flat object.

  ```mx
  post /upload {
    let f = request.files?.avatar
    write_file("./uploads/${f.name}", f.content)
    return json({ saved: f.name, bytes: f.size })
  }
  ```
- **Schedulers**:
  - `every(duration, fn)` — run `fn()` periodically. Returns a stop fn.
  - `after(duration, fn)` — run once after a delay. Returns a cancel fn.
  - `debounce(duration, fn)` — wrapper that fires only once `duration`
    has passed since the last call.
- **HTML helpers**: `html_escape` / `html_unescape` for XSS-safe templating.
- **`slug(s)`**: turn `"Hello, World!"` into `"hello-world"` for URL-safe IDs.

[0.13.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.13.0

## [0.12.0] — 2026-05-02

### Added
- **Server-sent events (SSE)** as a first-class route type:

  ```mx
  sse /events {
    while (true) {
      send({ tick: now() })
      sleep(1000)
    }
  }
  ```

  Real-time streaming over plain HTTP. Works with any browser via the
  built-in `EventSource` API — no WebSocket dependency. The body is
  invoked once per connection with a `send(value)` function that
  JSON-encodes and immediately flushes a single SSE frame.

[0.12.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.12.0

## [0.11.0] — 2026-05-02

### Added
- **JSON KV store** — five new built-ins for single-file persistence:
  `kv_get`, `kv_set`, `kv_delete`, `kv_keys`, `kv_clear`. Each operation
  reads → mutates → writes-to-tmp → renames atomically, with a process-wide
  mutex so concurrent route handlers can't corrupt the file.

  ```mx
  let DB = "./data.json"
  kv_set(DB, "user:${id}", { name: "Jassim" })
  let user = kv_get(DB, "user:${id}")
  ```
- **`examples/todo_api.mx`** — a 100-line full-stack showcase. JWT auth,
  CORS, access logging, KV persistence, validation. Use it as a
  starting point or a feature tour.

### Notes
The KV store is fine for prototypes and hobby apps. For production
scale, SQLite via `database/sql` remains on the v0.x roadmap.

[0.11.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.11.0

## [0.10.0] — 2026-05-02

### Added
The first production-readiness release. The `server` block now accepts
several new keys, all optional with sensible defaults:

```mx
server {
  port: 8080,
  read_timeout: "5s",          # number = ms, string = time.ParseDuration
  write_timeout: "30s",
  max_body: "10MB",            # number = bytes, string = KB / MB / GB

  tls: { cert: "./cert.pem", key: "./key.pem" },

  log: true,                   # one log line per request
  cors: {
    origins: ["https://app.example.com"],
    methods: ["GET", "POST"],
    headers: ["Content-Type", "Authorization"],
    credentials: true,
    max_age: 3600
  }
}
```

- **Graceful shutdown**: SIGINT / SIGTERM trigger a clean shutdown
  with up to 10s for in-flight requests to drain.
- **TLS / HTTPS**: when `tls.cert` and `tls.key` are set, the server
  boots `ListenAndServeTLS` and the startup banner shows `https://`.
- **Body size limits**: requests over `max_body` get a 413 (cheap
  Content-Length check first, MaxBytesReader as backstop for chunked).
- **Read / write timeouts** on the underlying http.Server.
- **Access logging** via `log: true` — one line per request showing
  method, path, status, and duration.
- **CORS handling** including OPTIONS preflight, configurable
  origins / methods / headers / credentials / max_age.

[0.10.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.10.0

## [0.9.0] — 2026-05-02

### Added
- **Shorthand HTTP routes**: `get /users { ... }` is sugar for
  `route GET /users { ... }`. Supports `get`, `post`, `put`, `delete`,
  `patch`, `head`, `options`. The verbose form keeps working — they
  parse to the same AST node.
- **Functional iterators**:
  - `sort(arr)`, `sort_by(arr, key_fn)`
  - `reduce(arr, fn, init)`
  - `sum(arr)`
  - `group_by(arr, key_fn)` → object of `key -> [items]`
  - `unique(arr)`, `flatten(arr)`, `zip(a, b)`
- **String helpers**: `pad_left`, `pad_right`, `repeat`, `substr`
  (negative-start aware), `index_of`.
- **Math helpers**: `pow`, `sqrt`, `log`, `exp`. Plus a `math` namespace
  with `math.PI`, `math.E`, `math.INFINITY`, `math.NAN`.

[0.9.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.9.0

## [0.8.0] — 2026-05-02

### Added
- **Call-stack tracebacks**: runtime errors now include the active
  function call chain. Each frame shows the function name and the
  source location where it was invoked. Matches Python / Node / Go
  conventions (failing function at top).
- **`mx run --eval '<src>'`** (alias `-e`): run an inline MX snippet
  with no file. Great for one-liners and shell glue.
- **New stdlib output helpers**: `write(...)` (no trailing newline)
  and `eprint(...)` (writes to stderr).
- **`env_required(name)`**: like `env()` but throws a descriptive
  error if the variable is unset or empty — used at startup to
  fail-fast on misconfiguration.

[0.8.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.8.0

## [0.7.0] — 2026-05-02

### Added
- **`try` as expression**: `let parsed = try { json_parse(s) } catch (e) { default }`.
  The value of the last expression in whichever block ran becomes the result.
- **Indexed loops**: `loop arr as i, item { ... }` exposes the 0-based
  position alongside each element. The single-var form keeps working.
- **Number literal forms**: hex `0xFF`, binary `0b1010`, octal `0o755`,
  and underscore separators `1_000_000` for readability.
- **Signed-cookie sessions**: `sign_cookie(secret, value)` produces a
  tamper-evident string; `verify_cookie(secret, signed)` returns the
  value or `null`. A cheaper alternative to JWT when you just need
  integrity.

[0.7.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.7.0

## [0.6.0] — 2026-05-02

### Added
- **`mx test`**: a built-in test runner. Files matching `*_test.mx` are
  discovered automatically; functions whose names start with `test_`
  are executed in isolation (each in a fresh interpreter). Exits non-zero
  on any failure, so it slots straight into CI.

  ```mx
  fn test_addition() { assert_eq(1 + 1, 2) }
  fn test_split()    { assert(len(split("a,b", ",")) == 2) }
  ```

  ```
  $ mx test
  examples/foo_test.mx
    ✓ addition
    ✓ split
  ✓ 2 passed in 3ms
  ```
- **Assertions**: `assert(cond, msg?)` and `assert_eq(a, b, msg?)`. The
  `_eq` variant prints both values on failure for an instant diff.
- **URL helpers**: `parse_url(s)` returns a structured object with
  `scheme`, `host`, `port`, `path`, `query`, `fragment`, `raw`.
  Plus `url_encode` / `url_decode`.
- **Date helpers**: `parse_date(s, layout?)` parses to Unix milliseconds;
  `format_date(ms, layout?)` is the inverse. Both default to RFC 3339
  and accept any Go reference layout.
- **`retry(fn, attempts, delay_ms?)`**: call `fn()` up to `attempts`
  times, returning the first non-error result. Useful for flaky
  third-party API calls.

[0.6.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.6.0

## [0.5.0] — 2026-05-02

### Added
- **Pattern matching** with `match`:

  ```mx
  let label = match value {
    1 => "one"
    2 => "two"
    _ => "other"
  }
  ```

  New `=>` arrow operator. `_` is the wildcard arm. Returns `null` if
  no arm matches.
- **JWT (HS256)** in stdlib:
  - `jwt.sign(payload, secret)` returns a signed token.
  - `jwt.verify(token, secret)` returns the payload object — or `null`
    if the signature is invalid or the `exp` claim has passed.
- **Regex** built-ins (Go's RE2 engine — no catastrophic backtracking):
  - `re_match(pattern, s)` → bool
  - `re_find(pattern, s)` → string (or array of capture groups)
  - `re_find_all(pattern, s)` → array of all matches
  - `re_replace(pattern, s, repl)` → string
- **HMAC**: `hmac_sha256(secret, message)` returns a hex digest.
- **Editor support**:
  - TextMate grammar at `extras/syntax/mxscript.tmLanguage.json` —
    works in any editor that speaks TextMate.
  - VS Code extension scaffold in `extras/vscode/`.
  - `.gitattributes` declares `.mx` so GitHub treats it as detectable.
  - `extras/linguist/` carries the grammar + sample files needed for
    a future PR to github-linguist (the project that powers GitHub's
    syntax highlighting).

[0.5.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.5.0

## [0.4.0] — 2026-05-02

### Added
- **Optional chaining** `?.`: `user?.profile?.city` returns `null` if any
  link in the chain is `null`, instead of throwing.
- **Nullish coalescing** `??`: `name ?? "anonymous"` falls back only on
  `null` (unlike `||`, which falls through on every falsy value such as
  `0` or empty string).
- **Cookies**:
  - Read: `request.cookies` is an object of `name -> value`, populated
    from the `Cookie` header on every request.
  - Write: `json` / `text` / `html` / `status` accept an optional trailing
    opts object with `cookies` and `headers`:

    ```mx
    return json({ ok: true }, {
      cookies: [{ name: "session", value: "abc",
                  path: "/", max_age: 3600, http_only: true,
                  same_site: "Lax" }],
      headers: { "X-Auth": "ok" }
    })
    ```
- **Pretty JSON**: `json_stringify(value, true)` returns indented output.

[0.4.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.4.0

## [0.3.0] — 2026-05-02

### Added
- **Static file serving**: `static "./public"` mounts a directory at `/`.
  Add `at "/cdn"` for a custom mount prefix. `index.html` is served
  automatically for directory requests; path-traversal attempts return 403.
- **Spread operator** `...`: works in array literals, object literals,
  and function call arguments.

  ```mx
  let combined = [...a, ...b, 7]
  let merged   = { ...base, ...extra, x: 1 }
  sum(...nums)
  ```
- **File I/O built-ins**: `read_file`, `write_file`, `file_exists`,
  `list_files`, `delete_file`.
- **Crypto / encoding**: `hash_sha256`, `base64_encode`, `base64_decode`.
- **IDs**: `uuid()` returns an RFC 4122 v4 UUID using `crypto/rand`.
- **Time**: `now_iso()` returns an ISO 8601 / RFC 3339 timestamp.

### Changed
- Spread in object literals merges later keys over earlier ones while
  preserving the *original* insertion position of duplicated keys
  (matches JavaScript semantics).

[0.3.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.3.0

## [0.2.0] — 2026-05-02

### Added
- **String interpolation**: `"Hello, ${name}! 2 + 3 = ${2 + 3}"`. Interpolated
  expressions are full MX expressions, including member access and function
  calls. Escape with `\${...}` for a literal `${`.
- **`while` loops**: `while (cond) { ... }` complements the existing
  `loop ... as` form for cases where the iteration count isn't known up front.
- **`break` and `continue`**: standard loop control, valid inside both
  `loop ... as` and `while`.
- **Pretty error messages**: parse and runtime errors now render with the
  offending source line in red and a caret (`^`) pointing at the column.
  Errors include a structured `--> file:line:col` location.
- **`mx repl`**: an interactive read-eval-print loop with multi-line input,
  expression results, and `.help` / `.exit` / `.clear` / `.vars` meta commands.

### Changed
- `parser` package now exports `ParseError` so callers (including the CLI)
  can render structured error context. `interpreter.MXError` already had
  `Line` / `Col` / `File` fields; both are now formatted by a single
  `printError` helper in the CLI.
- The `--port` flag now reliably wins over a program's `server { port: ... }`
  block. Previously the program's setting overrode the CLI flag.

### Internal
- New `Interpreter.Globals()` and `Interpreter.Exec()` exposed for embedding
  (used by the REPL).
- Built-ins are tagged via an `IsBuiltin` registry so the REPL's `.vars`
  command can hide them from output.

[0.2.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.2.0

## [0.1.0] — 2026-05-02

### Added
- Lexer that tokenizes `.mx` source with line/column tracking.
- Recursive-descent parser producing an AST.
- Tree-walking interpreter with closures, ordered objects, and a built-in
  HTTP router.
- HTTP routes: `route <METHOD> <path> { ... }` for `GET`, `POST`, `PUT`,
  `PATCH`, `DELETE`. Supports path params (`/users/:id`).
- `request` object inside route bodies: `method`, `path`, `headers`,
  `query`, `params`, `body` (auto-parsed for JSON / form bodies).
- `server { port: ..., host: ... }` configuration block.
- Middleware: `middleware name { ... }` plus `use name` inside routes.
- Control flow: `if` / `else` / `loop ... as`.
- `try` / `catch` with structured error objects.
- Standard library: `len`, `upper`, `lower`, `split`, `trim`, `contains`,
  `replace`, `starts_with`, `ends_with`, `push`, `pop`, `map`, `filter`,
  `find`, `join`, `reverse`, `range`, `keys`, `values`, `round`, `floor`,
  `ceil`, `abs`, `min`, `max`, `random`, `typeof`, type-check helpers,
  `json_parse`, `json_stringify`, `now`, `sleep`.
- HTTP response helpers: `json`, `text`, `html`, `status`, `redirect`.
- `fetch(url, opts)` for outbound HTTP calls.
- `env(name, default?)` for reading environment variables.
- AI namespace: `ai.complete(prompt)` and `ai.embed(text)` against the
  OpenAI-compatible API, configured via `OPENAI_API_KEY`.
- CLI: `mx run`, `mx init`, `mx build`, `mx version`, `mx help`.
- CLI flags: `--port`, `--watch` (hot reload), `--debug`.
- Local imports: `import "./utils.mx"`.

[0.1.0]: https://github.com/jlkdevelop/mxscript/releases/tag/v0.1.0
