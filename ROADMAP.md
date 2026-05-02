# MX Script Roadmap

Public roadmap. Vote on items via 👍 reactions on the linked issues.

> Founded by [Jassim Alkharafi](https://github.com/jlkdevelop). MX Script is MIT-licensed and built in the open.

---

## Shipped

- **v0.1.0** — initial release
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

## Next up

These are the things we'd most like help with.

- [ ] **Pre-built binaries** for macOS / Linux / Windows on every tag (GoReleaser).
- [ ] **`mx fmt`** — opinionated formatter for `.mx` files.
- [ ] **`mx repl`** — interactive REPL.
- [ ] **WebSocket routes** — `route WS /chat { ... }`.
- [ ] **SQLite driver** — `db.query("select ...")` as a built-in.
- [ ] **Cookies** — `request.cookies` and `set_cookie(name, value)`.
- [ ] **Static file serving** — `static "./public"` directive.
- [ ] **Better error messages** — show source line context in red, not just line numbers.

---

## Later

- [ ] **Package registry** — `mx install <pkg>` from GitHub.
- [ ] **LSP** — autocomplete, hover, go-to-definition in editors.
- [ ] **Compile to a standalone binary** — `mx compile app.mx -o app`.
- [ ] **Native concurrency** — green-thread style `spawn { ... }`.
- [ ] **Type hints** — optional gradual typing for IDEs.
- [ ] **Sessions / auth helpers** — JWT, OAuth2 flows in stdlib.

---

## Out of scope (for now)

- Full GC tuning knobs. The interpreter is fast enough for most APIs; if you need raw throughput, drop into Go.
- Object-oriented classes. MX uses plain objects + functions. Closures cover the use cases classes do.
- Templating engine. Use `text()` or `html()` and string-build, or call out to an external service.

---

## How to propose a roadmap item

Open an issue with the **feature** template, describe the use case, and link it here in a PR.
