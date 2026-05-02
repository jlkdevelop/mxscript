# Changelog

All notable changes to MX Script are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/).

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
