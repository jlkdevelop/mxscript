# Changelog

All notable changes to MX Script are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/).

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
