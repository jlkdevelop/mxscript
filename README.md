# MX Script

[![CI](https://github.com/jlkdevelop/mxscript/actions/workflows/ci.yml/badge.svg)](https://github.com/jlkdevelop/mxscript/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/jlkdevelop/mxscript)](https://goreportcard.com/report/github.com/jlkdevelop/mxscript)

> A modern, open-source scripting language for building web apps and APIs.
> One file. Zero boilerplate. Run with `mx run app.mx`.

**Founded and developed by [Jassim Alkharafi](https://github.com/jlkdevelop).**

```mx
server {
  port: 8080
}

let users = [
  { id: 1, name: "Jassim Alkharafi", role: "Founder" }
]

route GET /users {
  return json(users)
}

route GET /users/:id {
  let id = num(request.params.id)
  let user = find(users, fn(u) { return u.id == id })
  if (user == null) {
    return status(404, { error: "not found" })
  }
  return json(user)
}
```

That's it. No framework imports. No middleware setup. `mx run app.mx` and you have a JSON API on `localhost:8080`.

---

## Why MX Script?

Most languages make you choose: ergonomics or speed. JavaScript is fast to write but you assemble a framework, a runtime, a build tool, and a deploy story before you ship anything. Go is fast and simple but the syntax for a JSON API is still a hundred lines.

MX Script is opinionated: **the language is the framework**. Routes, JSON, middleware, env vars, and AI calls are all first-class. The interpreter is a single Go binary with zero runtime dependencies.

- Single-file apps. No `package.json`, no `Cargo.toml`, no `go.mod` from your perspective.
- HTTP server, JSON parsing, env vars, fetch, and AI built into the language.
- Clean syntax. `route GET /users { ... }` reads like English.
- One binary. No virtual machine, no GC tuning, no `node_modules`.
- MIT licensed. Built in the open. Pull requests welcome.

---

## Install

### Option 1 — pre-built binary (coming soon)

Download from the [releases page](https://github.com/jlkdevelop/mxscript/releases) and put `mx` on your `$PATH`.

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
route GET /             { return text("hello") }
route GET /users/:id    { return json({ id: request.params.id }) }
route POST /users       { return status(201, request.body) }
route DELETE /users/:id { return json({ deleted: true }) }
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

`ai.complete` reads `OPENAI_API_KEY` from the environment.

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

MX Script was created by **Jassim Alkharafi** (founder & lead developer). If MX Script is useful to you, a ⭐ on GitHub goes a long way.
