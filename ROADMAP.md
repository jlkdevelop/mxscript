# MX Script Roadmap

Public roadmap. Vote on items via 👍 reactions on the linked issues.

> Created by [Jassim Alkharafi](https://github.com/jlkdevelop). MX Script is MIT-licensed and built in the open.

---

## Shipped

- **v0.59.0** (2026-05-02)
  - `notify.slack` / `notify.discord` / `notify.email` — one-line outbound notifications with `{ ok, status, error }` results
  - String messages auto-shape (`text` for Slack, `content` for Discord); object messages pass through for rich payloads
- **v0.58.0** (2026-05-02)
  - `magic_link.create` / `verify` — signed, stateless, time-limited tokens for passwordless email sign-in
  - `totp.generate` / `verify` / `uri` — RFC 6238 TOTP (Google Authenticator compatible) with ±drift windows
  - Secret normalisation tolerates upper/lower case, spaces, and `=` padding variants
- **v0.57.0** (2026-05-02)
  - `cron(spec, fn)` builtin — Vixie 5-field cron with `*/step`, ranges, lists, OR semantics for DOM/DOW
  - `mx routes <file.mx>` lists every registered route without booting the HTTP server
  - Public `interpreter.RouteSummary() []RouteInfo` for embedders
- **v0.56.0** (2026-05-02)
  - `webhooks.*` namespace: `verify_stripe`, `verify_github`, `verify_svix`, `verify_shopify`, `verify_slack`
  - Stripe & Slack reject stale timestamps (default 300s tolerance) — defends against replay
  - Tests verify against each provider's documented signature samples
- **v0.55.0** (2026-05-02)
  - Template engine grew `{{#if}}`/`{{else}}`/`{{/if}}`, `{{#each}}`/`{{/each}}` (with `{{this}}`, `{{@index}}`, `{{@key}}`), and `{{> partial}}`
  - `render` / `render_string` accept a third `partials` argument (name → template-string map)
  - `examples/blog.mx` — complete server-rendered blog in ~60 lines
- **v0.54.0** (2026-05-02)
  - `ai.complete` & `ai.stream` now speak ten providers behind one API: OpenAI, Anthropic, Gemini, xAI Grok, Mistral, DeepSeek, Groq, OpenRouter, Together, Ollama
  - Dispatch-table architecture — adding a new OpenAI-compatible provider is one entry
  - Local-first via Ollama (no API key)
- **v0.53.0** (2026-05-02)
  - VM lowers `let`, `=`, `if`, `while` — tight loops now run 2–3× faster on `--bytecode`
  - New opcodes: `OpStoreVar`, `OpAssignVar`; jumps moved to absolute PC addressing
  - Public `interpreter.CompileBlock` and statement-level compile cache
- **v0.52.0** (2026-05-02)
  - Experimental stack-machine bytecode VM behind `mx run --bytecode` and `mx bench --bytecode`
  - Per-interpreter compile cache; transparent fall-back to the tree-walker for unsupported nodes
  - Public `interpreter.CompileExpr` / `Compiled.Run` API + parity tests
- **v0.51.0** (2026-05-02)
  - VS Code `.vsix` attached to every release via GoReleaser pre-build hook
  - github-linguist PR scaffold polished (`extras/linguist/languages.yml.patch`, brand color `#2B54A8`)
- **v0.50.0** (2026-05-02)
  - LSP signature help with active-parameter highlighting
  - LSP snippet completions for `route`, `server`, `if`, `while`, etc.
- **v0.49.0** (2026-05-02)
  - Redis client (`redis.open`, get/set/del/incr, pub/sub)
  - MySQL driver (`sql.open` auto-routes by DSN scheme)
- **v0.48.0** (2026-05-02)
  - `argon2id_hash` / `argon2id_verify` (RFC 9106) and `scrypt_hash` / `scrypt_verify`
  - `yaml_parse` / `yaml_stringify`, `toml_parse` / `toml_stringify`
- **v0.47.0** (2026-05-02)
  - `ai.vision(image, prompt)` — multimodal image+text completion
  - `ai.embed(text)` and `cosine_similarity(a, b)` for vector search
- **v0.46.0** (2026-05-02)
  - CSRF helpers: `csrf_token(secret, sid)` / `verify_csrf(...)`
  - In-process pub/sub: `pubsub.topic()` with subscribe / publish / count
- **v0.45.0** (2026-05-02)
  - README hero refreshed for the v0.44+ surface
  - `examples/full_app.mx` — kitchen-sink showcase
- **v0.44.0** (2026-05-02)
  - Route lookup is now O(segments) via a path-segment trie (was linear)
- **v0.43.0** (2026-05-02)
  - `mx upgrade` — self-update from latest GitHub release
  - `mx doctor` — env / install / network diagnostics
- **v0.42.0** (2026-05-02)
  - Namespaced imports: `import "./auth.mx" as auth` — proper module encapsulation
- **v0.41.0** (2026-05-02)
  - `mx new <template>` — five opinionated starters: api / todo / chat / ai / blog
- **v0.40.0** (2026-05-02)
  - Durable background jobs: `jobs.create / enqueue / process / stats / close`
  - SQLite-backed, retries with exponential backoff, delayed scheduling
- **v0.39.0** (2026-05-02)
  - Route groups: `group /api/v1 { use auth; get /users { ... } }` — shared path prefix + middleware
- **v0.38.0** (2026-05-02)
  - `validate(value, schema)` — JSON-Schema-lite input validation
- **v0.37.0** (2026-05-02)
  - AI tool calling: `ai.complete(prompt, { tools, messages })` — structured tool_call response
  - `examples/agent.mx` — 60-line tool-calling agent
- **v0.36.0** (2026-05-02)
  - PostgreSQL support — `sql.open` auto-routes by DSN scheme
  - `status_page(opts?)` health dashboard
- **v0.35.0** (2026-05-02)
  - `mx bench` runner — auto-scales iterations to a 1s budget
  - `fs.watch(path, fn)` polling directory watcher
- **v0.34.0** (2026-05-02)
  - `swagger_ui(spec_url)` / `redoc_ui(spec_url)` interactive API docs
  - `sql.migrate(db, [...])` schema versioning (hash-tracked, idempotent)
- **v0.33.0** (2026-05-02)
  - Gemini AI provider (`ai.complete(prompt, { provider: "gemini" })`)
  - `sql.transaction(db, fn)` with auto-rollback on throw
- **v0.32.0** (2026-05-02)
  - `openapi(info?)` auto-generates an OpenAPI 3.1 spec from registered routes
  - `routes()` returns an array of `{ method, path }`
- **v0.31.0** (2026-05-02)
  - `mx test --cover` — line coverage report
- **v0.30.0** (2026-05-02)
  - `session.create / read / clear` — high-level session API
  - `examples/chat.mx` — 80-line real-time chat showcase (WS + sessions + HTML client)
- **v0.29.0** (2026-05-02)
  - LSP hover with signatures + summaries for ~110 builtins
  - LSP completion (builtins + keywords)
- **v0.28.0** (2026-05-02)
  - Destructure renaming (`{ name: n }`), defaults (`{ name = "anon" }`), array rest (`[head, ...rest]`)
- **v0.27.0** (2026-05-02)
  - Destructuring: `let { a, b } = obj` / `let [x, y] = arr`
  - `time_ago(ms)` and `time_human(ms)` for friendly time strings
- **v0.26.0** (2026-05-02)
  - `password.hash` / `password.verify` (PBKDF2-SHA256, stdlib only)
  - `aes_encrypt` / `aes_decrypt` (AES-256-GCM)
  - `ai.stream(prompt, on_chunk)` for streaming LLM responses
- **v0.25.0** (2026-05-02)
  - `shell(cmd, args?, opts?)` — subprocess execution
  - CSV: `csv_parse` / `csv_stringify`
  - `format(fmt, ...args)` — printf-style
  - "Did you mean ..." suggestions on undefined-identifier errors
- **v0.24.0** (2026-05-02)
  - Language Server (`mx lsp`) — diagnostics + format-on-save + hover stub
  - VS Code extension declares LSP integration
- **v0.23.0** (2026-05-02)
  - Docs site at mxscript.com (`site/`, GitHub Pages)
  - Landing page + single-page docs with sticky sidebar
- **v0.22.0** (2026-05-02)
  - SQLite via `sql.open / exec / query / query_one / close` (modernc.org/sqlite, pure Go)
  - New `KindHandle` value type for opaque resources
- **v0.21.0** (2026-05-02)
  - WebSockets (`ws /path { ... }`) — RFC 6455 in pure stdlib
  - `recv()` / `send()` / `close()` injected into ws route bodies
- **v0.20.0** (2026-05-02)
  - `mx fmt` — opinionated formatter (token-based, comment-preserving)
  - Lexer `CollectComments` flag for tooling
- **v0.19.0** (2026-05-02)
  - `image.info` / `image.resize` / `image.convert` (PNG / JPEG, GIF input)
- **v0.18.0** (2026-05-02)
  - OAuth2 helpers: `oauth.authorize_url(opts)` / `oauth.exchange_code(opts)`
  - Built-in providers: Google, GitHub, Discord, LinkedIn, Microsoft
- **v0.17.0** (2026-05-02)
  - gzip compression: `server.compression: true`
  - `markdown(s)` — built-in CommonMark subset → safe HTML
  - Anthropic Claude provider: `ai.complete(..., { provider: "anthropic" })`
- **v0.16.0** (2026-05-02) — concurrency
  - `spawn { ... }` runs a block in a goroutine
  - Channels: `chan(cap?)`, `send`, `recv`, `close_chan`
  - `wait_group()` for fork/join coordination
  - Env is now thread-safe via RWMutex
- **v0.15.0** (2026-05-02)
  - Email via SMTP: `email.send({ host, from, to, subject, body, html?, ... })`
  - Rate limiting: `server.rate_limit: { requests: N, per: "1m" }` (per-IP token bucket)
  - Webhook verification: `verify_webhook(secret, body, sig, scheme)` — hex / base64 / github / stripe
- **v0.14.0** (2026-05-02)
  - Templates: `render(path, vars)` / `render_string(tmpl, vars)` with `{{ }}` and `{{{ }}}` placeholders
  - Structured logger: `log.info` / `log.warn` / `log.error` / `log.debug`
  - Date arithmetic: `add_days`, `add_hours`, `add_minutes`, `days_between`, `weekday`
  - Request helpers: `request.bearer_token`, `request.is_json`, `request.ip`
  - Vercel adapter: `mx build --vercel`
  - Public embedder API: `Interpreter.Load`, `Interpreter.Handler`, `Interpreter.HasRoutes`
- **v0.13.0** (2026-05-02)
  - Multipart file uploads via `request.files`
  - Schedulers: `every` / `after` / `debounce`
  - `html_escape` / `html_unescape` / `slug`
- **v0.12.0** (2026-05-02)
  - Server-sent events: `sse /events { send(...) }`
- **v0.11.0** (2026-05-02)
  - JSON KV store (kv_get / kv_set / kv_delete / kv_keys / kv_clear)
  - `examples/todo_api.mx` — 100-line full-stack showcase
- **v0.10.0** (2026-05-02) — production hardening
  - TLS / HTTPS via `server.tls.{cert, key}`
  - Graceful shutdown on SIGINT / SIGTERM (10s drain)
  - Request body limits (`server.max_body`)
  - Read / write timeouts (`server.read_timeout`, `server.write_timeout`)
  - Access logging (`server.log`)
  - CORS with preflight (`server.cors`)
- **v0.9.0** (2026-05-02)
  - Shorthand routes: `get /users { ... }` etc.
  - Functional iterators: sort / sort_by / reduce / sum / group_by / unique / flatten / zip
  - String helpers: pad_left / pad_right / repeat / substr / index_of
  - Math: pow / sqrt / log / exp + `math.PI` / `math.E` namespace
- **v0.8.0** (2026-05-02)
  - Call-stack tracebacks on runtime errors
  - `mx run --eval '<src>'` for inline one-liners
  - `write` / `eprint` stdlib output helpers
  - `env_required(name)` for fail-fast config validation
- **v0.7.0** (2026-05-02)
  - `try` as expression
  - Indexed loops: `loop arr as i, item { ... }`
  - Hex / binary / octal literals + underscore separators (`0xFF`, `1_000_000`)
  - Signed-cookie sessions: `sign_cookie` / `verify_cookie`
- **v0.6.0** (2026-05-02)
  - `mx test` — built-in test runner with `assert` / `assert_eq` builtins
  - URL parsing (`parse_url`, `url_encode`, `url_decode`)
  - Date helpers (`parse_date`, `format_date`)
  - `retry(fn, attempts, delay_ms?)` for flaky calls
- **v0.5.0** (2026-05-02)
  - Pattern matching with `match` / `=>`
  - JWT (HS256) — `jwt.sign` / `jwt.verify` (honours `exp`)
  - Regex — `re_match`, `re_find`, `re_find_all`, `re_replace`
  - `hmac_sha256(secret, msg)` general-purpose HMAC
  - TextMate grammar + VS Code extension + linguist PR scaffolding
- **v0.4.0** (2026-05-02)
  - Optional chaining (`?.`) and nullish coalescing (`??`)
  - Cookies: `request.cookies` (read) and response opts (write)
  - Pretty JSON via `json_stringify(v, true)`
- **v0.3.0** (2026-05-02)
  - Static file serving via `static "./public"` directive
  - Spread operator (`...`) for arrays, objects, and call arguments
  - File I/O built-ins: `read_file`, `write_file`, `file_exists`, `list_files`, `delete_file`
  - Crypto / encoding: `hash_sha256`, `base64_encode`, `base64_decode`
  - `uuid()` (RFC 4122 v4) and `now_iso()` (RFC 3339)
- **v0.2.0** (2026-05-02)
  - String interpolation (`"hello ${name}"`)
  - `while` loops, `break`, `continue`
  - Pretty error messages with source-line context and caret pointers
  - Interactive REPL (`mx repl`)
- **v0.1.0** (2026-05-02) — initial release
  - Lexer, parser, tree-walking interpreter
  - HTTP routes (`GET` / `POST` / `PUT` / `DELETE` / `PATCH`)
  - Path params (`/users/:id`), query, headers, JSON body
  - Middleware via `use` keyword
  - Control flow (`if` / `else` / `loop ... as`)
  - `try` / `catch`
  - Standard library: strings, arrays, math, types, JSON
  - `ai.complete` and `ai.embed` (OpenAI-compatible)
  - CLI: `mx run`, `mx init`, `mx build`, `--port`, `--watch`, `--debug`
  - Local imports: `import "./utils.mx"`

---

## Next up (v0.8 candidates)

These are the things we'd most like help with.

- [ ] **`mx fmt`** — opinionated formatter for `.mx` files (needs comment-preserving lexer first).
- [ ] **WebSocket routes** — `route WS /chat { ... }`.
- [ ] **SQLite driver** — `db.query("select ...")` as a built-in.
- [ ] **`mx test --cover`** — line-coverage report.
- [ ] **`spawn { ... }`** — green-thread concurrency.
- [ ] **GitHub linguist PR** — once we hit the public-adoption threshold.
- [ ] **VS Code Marketplace listing** — publish the extension.

---

## Later

- [ ] **Package registry** — `mx install <pkg>` from GitHub.
- [ ] **LSP** — autocomplete, hover, go-to-definition in editors.
- [ ] **Compile to a standalone binary** — `mx compile app.mx -o app`.
- [ ] **Native concurrency** — green-thread style `spawn { ... }`.
- [ ] **Type hints** — optional gradual typing for IDEs.
- [ ] **Sessions / auth helpers** — JWT, OAuth2 flows in stdlib.

---

## The 1.0 milestone — self-hosted MX

Once the language is stable enough, the goal is to **rewrite the MX compiler in MX itself** (a process called *bootstrapping* or *self-hosting*). At that point MX no longer needs Go to exist — it can build itself. This is the milestone where MX graduates from a project-built-in-Go to a real language with its own ecosystem.

Path to get there:

1. v0.x — grow the language: types, modules, more stdlib, performance work
2. v0.9 — write `mxc` (the MX-in-MX compiler) as a side project, validated against the Go reference
3. v1.0 — `mxc` produces output bit-identical to the Go reference for the full test suite. Ship `mxc` as the canonical build, keep the Go interpreter for development.

This is how every serious language graduates. Python's PyPy, Go's `gc`, Rust's `rustc`, TypeScript's `tsc` — all started life implemented in another language and self-hosted later.

---

## Out of scope (for now)

- Full GC tuning knobs. The interpreter is fast enough for most APIs; if you need raw throughput, drop into Go.
- Object-oriented classes. MX uses plain objects + functions. Closures cover the use cases classes do.
- Templating engine. Use `text()` or `html()` and string-build, or call out to an external service.

---

## How to propose a roadmap item

Open an issue with the **feature** template, describe the use case, and link it here in a PR.
