<p align="center">
  <img src="assets/logo.png" alt="MX Script" width="220">
</p>

<h1 align="center">MX Script</h1>

<p align="center">
  <a href="https://github.com/jlkdevelop/mxscript/actions/workflows/ci.yml"><img src="https://github.com/jlkdevelop/mxscript/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="MIT License"></a>
  <a href="https://goreportcard.com/report/github.com/jlkdevelop/mxscript"><img src="https://goreportcard.com/badge/github.com/jlkdevelop/mxscript" alt="Go Report"></a>
</p>

<p align="center">
  <strong>A lightweight scripting language for building one-file web APIs.</strong><br>
  No framework. No build step. No <code>node_modules</code>. Run with <code>mx run app.mx</code>.
</p>

<p align="center">
  <strong>🎉 v1.0 — production-ready stable release.</strong><br>
  Created and maintained by <a href="https://github.com/jlkdevelop">Jassim Alkharafi</a>.
</p>

```mx
server { port: 8080 }

let db = sql.open("./users.db")
sql.migrate(db, [
  "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)"
])

let SCHEMA = {
  type: "object",
  required: ["name", "email"],
  properties: {
    name:  { type: "string", min_length: 2 },
    email: { type: "string", format: "email" }
  }
}

group /api/v1 {
  get /users {
    let p     = paginate(request)
    let total = sql.count(db, "users", {})
    let items = sql.find(db, "users", {}, { order: "id DESC", limit: p.limit, offset: p.offset })
    return json(page_response(items, p, total))
  }

  get /users/:id {
    let u = sql.find_one(db, "users", { id: num(request.params.id) })
    if (u == null) { return problem(404, "User not found") }
    return json(u)
  }

  post /users {
    let r = body_validate(request, SCHEMA)
    if (!r.ok) { return r.response }
    let id = sql.insert(db, "users", r.body).last_insert_id
    return status(201, { id: id })
  }
}

get /openapi.json { return json(openapi({ title: "Users API" })) }
get /docs        { return swagger_ui("/openapi.json") }
```

That's a paginated, validated REST API with RFC 7807 error responses, auto-generated OpenAPI docs, and SQLite persistence — in 30 lines. `mx run app.mx` and you're done.

---

## Why MX Script?

The simple case — *"I want a JSON endpoint that does one useful thing"* — gets buried under setup. Before writing the thing you actually wanted to build, you assemble a runtime, a framework, a router, a build tool, a deploy story, and twenty config files.

MX Script collapses that. **The language is the framework.** Routes, JSON, SQL, JWT, OAuth, WebSockets, AI, and background jobs are first-class syntax. The interpreter is a single Go binary, ~5 MB. Drop a `.mx` file on disk, run `mx run app.mx`, and you have a real HTTP server.

It's intentionally small and opinionated — built for the 80% case where you want **one file, one binary, one command to ship**.

### What ships in the box

| Layer | What you get |
|---|---|
| **Web framework** | Routes (`get /users { ... }`), middleware, route groups, static files, cookies, CORS, gzip, rate limiting (global + per-key), body limits, timeouts, graceful shutdown, TLS |
| **Real-time** | Server-sent events (`sse /events`) and WebSockets (`ws /chat`) — both pure stdlib, no external dependencies |
| **Database** | SQLite + Postgres + MySQL through one `sql` namespace. Transactions, hash-tracked migrations, parameterized queries. Full-text search via `search.*` (FTS5) |
| **Auth** | JWT, signed-cookie sessions, OAuth2 (Google / GitHub / Discord / LinkedIn / Microsoft), `magic_link.*` (passwordless email), `totp.*` (RFC 6238 — Google Authenticator compatible), `password.hash` (PBKDF2 / Argon2id / scrypt), AES-256-GCM, `webhooks.*` signature verification (Stripe / GitHub / Svix / Shopify / Slack) |
| **AI** | 10 providers behind one API: OpenAI / Anthropic / Gemini / xAI Grok / Mistral / DeepSeek / Groq / OpenRouter / Together / Ollama. `ai.complete`, `ai.stream`, `ai.embed`, `ai.vision`, `ai.image` (DALL-E), `ai.transcribe` (Whisper) |
| **Payments** | `stripe.*` — checkout, customer, customer_portal, subscription. Pairs with `webhooks.verify_stripe` for the full SaaS loop |
| **Notifications** | `notify.slack` / `notify.discord` / `notify.email` (Resend) — string or rich-payload sends with `{ ok, status, error }` results |
| **Object storage** | `s3.*` — pure-Go AWS Signature V4. Works with AWS S3, Cloudflare R2, Backblaze B2, DigitalOcean Spaces, MinIO, Wasabi |
| **Background jobs + cron** | Durable, SQLite-backed queue with retries and exponential backoff. `cron(spec, fn)` for Vixie 5-field schedules |
| **Observability** | `metrics.counter / gauge / histogram` + auto `/metrics` endpoint in Prometheus exposition format. Pairs with the existing `log.*` and request logging |
| **API tooling** | `openapi()` auto-generates a 3.1 spec from your routes. `swagger_ui()` and `redoc_ui()` mount interactive docs in one line. `mx routes` lists every route without booting the server |
| **Templates** | Mustache-style with `{{#if}}`, `{{#each}}`, `{{> partial}}`, auto-escape default. `render(path, vars, partials)` returns an HTML response |
| **Stdlib (~200 fns)** | Strings, arrays, math, JSON, regex, file I/O, image manipulation (`thumbnail`/`crop`/`resize`/`convert`), markdown, CSV, email (SMTP), templates, schedulers, validation, subprocess, fs.watch + glob, time + path utilities, IDs (`uuid`/`ulid`/`nanoid`/`snowflake`), random + base32, `pick` / `omit` / `merge` / `deep_merge` |
| **Concurrency** | `spawn { ... }` goroutines, channels, `wait_group`, thread-safe environments |
| **Performance** | Bytecode VM behind `mx run --bytecode` lowers expressions, function bodies, control flow, calls, member access, optional chaining, short-circuit, break/continue. ~2-3× faster on tight loops; falls back transparently for unsupported nodes |
| **Tooling** | `mx run / init / new / build / repl / test [--cover] / bench / fmt / lsp / check / pkg / serve / ci / examples / help / docs / routes / upgrade / doctor` |
| **Editor support** | TextMate grammar, VS Code extension (auto-built into every release), full LSP (diagnostics, format-on-save, hover, completion, signature help, snippets — all 200+ builtins) |
| **Distribution** | GoReleaser binaries on every tag, Homebrew tap, `mx build --vercel` / `--docker` / `--fly` / `--railway` / `--wasm` for one-command deploys |
| **Browser** | `mx build --wasm` produces `dist/mx.wasm` + JS shim. Full interpreter runs client-side. Live playground at [`site/playground/`](site/playground/) |

### Nine starter projects, one command

```bash
mx new api my-api          # REST showcase: paginate + uploads + api-key auth + OpenAPI
mx new shortener my-short  # URL shortener in 50 lines (the canonical demo)
mx new todo my-todos       # JWT-authenticated SQLite todo with sql.find/insert/update
mx new chat realtime       # WebSocket chat with a built-in browser client
mx new ai my-bot           # Tool-calling agent exposed as POST /chat
mx new blog my-blog        # SSR markdown blog with admin + JSON twin
mx new saas my-saas        # Magic-link auth + Stripe + /metrics + cron + admin
mx new dashboard my-admin  # Admin dashboard with WebSocket live charts
mx new react my-app        # Vite + React frontend with an MX backend at /api/*
```

Each scaffolds a complete, runnable app. Read the source, change the bits you don't like, ship.

---

## Try it in your browser

The full interpreter compiles to WebAssembly. Run a program without installing anything:

```bash
mx build --wasm                      # produces dist/mx.wasm + wasm_exec.js
# then serve site/playground/ — open the result in any modern browser
```

The playground at [`site/playground/`](site/playground/) ships a complete editor + run loop with bundled examples (closures, map/filter, match, JSON, tight numeric loops). See [`site/playground/README.md`](site/playground/README.md) for deploy instructions.

## Install

### Option 1 — one-liner install

```bash
curl -fsSL https://raw.githubusercontent.com/jlkdevelop/mxscript/main/scripts/install.sh | bash
```

Detects your OS / arch, downloads the latest signed release binary, and drops it at `$HOME/.mx/bin/mx`. Add that directory to your `$PATH` and you're done. Pin a specific version with `MX_VERSION=v0.77.0`.

### Option 2 — build from source

Requires Go 1.21+.

```bash
git clone https://github.com/jlkdevelop/mxscript.git
cd mxscript
go build -o mx .
./mx version
```

### Option 3 — go install

```bash
go install github.com/jlkdevelop/mxscript@latest
```

The binary is named `mxscript` after `go install`. Rename or alias it to `mx`:

```bash
mv $(go env GOPATH)/bin/mxscript $(go env GOPATH)/bin/mx
```

---

## Quickstart

```bash
mx init my-api
cd my-api
mx run app.mx
```

Then in another terminal:

```bash
curl http://localhost:8080/
curl http://localhost:8080/hello/Jassim
```

---

## CLI

```
mx run <file.mx>               # run an MX Script program
mx run <file.mx> --port 3000   # override the server port
mx run <file.mx> --watch       # restart on file changes (hot reload)
mx run <file.mx> --debug       # print tokens and AST
mx test [path]                 # run *_test.mx files
mx init [name]                 # scaffold a new project
mx build <file.mx>             # parse & validate without running
mx repl                        # interactive REPL
mx version                     # print version
mx help                        # show help
```

---

## Language tour

### Variables

```mx
let name = "Jassim"
let age = 30
let active = true
let scores = [10, 20, 30]
let user = { id: 1, name: "Ada" }

// String interpolation
print("Hello, ${name}! Score sum: ${scores[0] + scores[1] + scores[2]}")

// Optional chaining + nullish coalescing
let city = user?.profile?.city ?? "unknown"
```

### Functions

```mx
fn greet(name) {
  return "Hello, " + name
}

print(greet("world"))
```

Anonymous functions:

```mx
let doubled = map([1, 2, 3], fn(n) { return n * 2 })
```

### Control flow

```mx
if (age >= 18) {
  print("adult")
} else {
  print("minor")
}

loop scores as s {
  print(s)
}

loop 5 as i {
  print(i)
}

let n = 0
while (n < 100) {
  n = n + 1
  if (n == 50) { break }
}
```

### HTTP routes

```mx
// Shorthand form (recommended)
get /             { return text("hello") }
get /users/:id    { return json({ id: request.params.id }) }
post /users       { return status(201, request.body) }
delete /users/:id { return json({ deleted: true }) }

// Verbose form (equivalent)
route GET /users  { return json(users) }
```

### Middleware

```mx
middleware auth {
  if (request.headers["authorization"] != "Bearer secret") {
    return status(401, { error: "unauthorized" })
  }
}

route POST /admin {
  use auth
  return json({ ok: true })
}
```

### Built-in functions

| Category   | Functions |
|------------|-----------|
| Output     | `print`, `println` |
| HTTP       | `json`, `text`, `html`, `status`, `redirect`, `fetch` |
| Env        | `env(name)` |
| String     | `len`, `upper`, `lower`, `split`, `trim`, `contains`, `replace`, `starts_with`, `ends_with` |
| Array      | `push`, `pop`, `map`, `filter`, `find`, `join`, `reverse`, `range` |
| Math       | `round`, `floor`, `ceil`, `abs`, `min`, `max`, `random` |
| Types      | `typeof`, `isString`, `isNumber`, `isBool`, `isNull`, `isArray`, `isObject` |
| JSON       | `json_parse`, `json_stringify(v, pretty?)` |
| File I/O   | `read_file`, `write_file`, `file_exists`, `list_files`, `delete_file` |
| Crypto     | `hash_sha256`, `hmac_sha256`, `base64_encode`, `base64_decode`, `uuid` |
| Regex      | `re_match`, `re_find`, `re_find_all`, `re_replace` |
| JWT        | `jwt.sign(payload, secret)`, `jwt.verify(token, secret)` |
| URL        | `parse_url`, `url_encode`, `url_decode` |
| Time       | `now()`, `now_iso()`, `sleep(ms)`, `parse_date`, `format_date` |
| Test       | `assert(cond, msg?)`, `assert_eq(a, b, msg?)` |
| KV store   | `kv_get`, `kv_set`, `kv_delete`, `kv_keys`, `kv_clear` |
| Misc       | `retry(fn, attempts, delay_ms?)` |
| AI         | `ai.complete(prompt)`, `ai.embed(text)` |

See [docs/api.md](docs/api.md) for the full reference.

### AI built-ins

```mx
route POST /summarise {
  let summary = ai.complete("Summarise: " + request.body.text)
  return json({ summary: summary })
}
```

`ai.complete(prompt, opts?)` works against ten providers behind a single API. Pass `{ provider: "..." }` to switch:

| Provider | `provider:` value | API key env var |
|---|---|---|
| OpenAI (default) | `"openai"` | `OPENAI_API_KEY` |
| Anthropic Claude | `"anthropic"` | `ANTHROPIC_API_KEY` |
| Google Gemini | `"gemini"` / `"google"` | `GEMINI_API_KEY` |
| xAI Grok | `"grok"` / `"xai"` | `XAI_API_KEY` |
| Mistral | `"mistral"` | `MISTRAL_API_KEY` |
| DeepSeek | `"deepseek"` | `DEEPSEEK_API_KEY` |
| Groq | `"groq"` | `GROQ_API_KEY` |
| OpenRouter | `"openrouter"` | `OPENROUTER_API_KEY` |
| Together AI | `"together"` | `TOGETHER_API_KEY` |
| Ollama (local) | `"ollama"` | _none — runs locally_ |

`ai.stream(prompt, on_chunk, opts?)`, `ai.embed(text)`, `ai.vision(prompt, images)` and `ai.similarity(a, b)` round out the namespace. See `examples/ai_providers.mx` for a copy-pasteable cheat sheet.

### Pattern matching

```mx
let label = match status_code {
  200 => "OK"
  404 => "Not Found"
  500 => "Server Error"
  _   => "Status ${status_code}"
}
```

### Authentication with JWT

```mx
route POST /login {
  let token = jwt.sign({ sub: request.body.username, exp: now() / 1000 + 3600 }, env("JWT_SECRET"))
  return json({ token: token })
}

route GET /me {
  let claims = jwt.verify(request.headers["authorization"], env("JWT_SECRET"))
  if (claims == null) {
    return status(401, { error: "invalid token" })
  }
  return json(claims)
}
```

### Error handling

```mx
try {
  let data = json_parse(request.body.payload)
  return json(data)
} catch (e) {
  return status(400, { error: e.message })
}
```

---

## Deploy to Vercel

MX Script ships a built-in Vercel adapter. From any project with an `app.mx`:

```bash
mx build --vercel
git add main.go go.mod vercel.json
git commit -m "Deploy via mx build --vercel"
git push   # Vercel autodeploys on push
```

`mx build --vercel` generates three files at the project root:

| File          | Role                                                                 |
| ------------- | -------------------------------------------------------------------- |
| `main.go`     | A 30-line Go entrypoint that `//go:embed`s `app.mx`, lexes/parses/loads it via the interpreter library, and serves the resulting handler on `$PORT`. |
| `go.mod`      | Pins the mxscript runtime to the version of the CLI that generated the build. |
| `vercel.json` | Declares Vercel's Go framework preset so the build is detected automatically. |

Vercel does the rest: detects the Go module, compiles the binary, and runs it as a long-running serverless function on Fluid Compute. Your `.mx` source is the source of truth — re-run `mx build --vercel` anytime you upgrade the mx CLI.

> **Embedding MX in your own Go server?** The same building blocks that power the Vercel adapter — `interpreter.Load(prog)`, `interpreter.Handler()`, `interpreter.HasRoutes()` — are public. Drop an MX app into any Go binary that speaks `net/http`.

---

## Editor support

`.mx` files have full syntax highlighting in VS Code (and any TextMate-compatible editor):

```bash
git clone https://github.com/jlkdevelop/mxscript.git
cd mxscript/extras/vscode
code --install-extension .
```

The grammar lives at [`extras/syntax/mxscript.tmLanguage.json`](extras/syntax/mxscript.tmLanguage.json) — drop it into Sublime, Atom, or any other TextMate-aware editor.

GitHub itself doesn't yet recognise `.mx` natively (we're working through [github-linguist](https://github.com/github-linguist/linguist)), but the `.gitattributes` in this repo gives the best fallback.

---

## Project structure

```
mxscript/
├── main.go                 # CLI entry point
├── lexer/lexer.go          # tokenizer
├── parser/
│   ├── ast.go              # AST node types
│   └── parser.go           # recursive-descent parser
├── interpreter/
│   ├── interpreter.go      # tree-walking interpreter + HTTP server
│   ├── builtins.go         # standard library
│   └── parse_helper.go     # bridge for `import "./file.mx"`
├── examples/               # sample .mx programs
└── docs/                   # full language reference
```

---

## Roadmap

See [ROADMAP.md](ROADMAP.md). Highlights:

- [x] Lexer, parser, interpreter
- [x] HTTP routes (GET / POST / PUT / DELETE / PATCH)
- [x] `request.body`, `request.params`, `request.headers`, `request.query`
- [x] Middleware via `use`
- [x] Control flow (`if`, `else`, `loop ... as`)
- [x] `try` / `catch`
- [x] Standard library (string, array, math, JSON, types)
- [x] `ai.complete` / `ai.embed`
- [x] CLI: `--port`, `--watch`, `--debug`, `mx init`, `mx build`
- [x] Local imports: `import "./utils.mx"`
- [ ] Pre-built binary releases for macOS / Linux / Windows
- [ ] Package registry (`mx install pkg`)
- [ ] WebSocket routes
- [ ] Built-in SQLite driver
- [ ] LSP for editor support

---

## Contributing

MX Script is open source under the MIT license. Pull requests, issues, and discussions are welcome.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow. The TL;DR:

```bash
git clone https://github.com/jlkdevelop/mxscript.git
cd mxscript
go build -o mx .
go test ./...
./mx run examples/app.mx
```

---

## License

[MIT](LICENSE) © Jassim Alkharafi

---

## Credits

MX Script was created by **Jassim Alkharafi** (creator & lead developer). If MX Script is useful to you, a ⭐ on GitHub goes a long way.
