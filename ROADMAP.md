# MX Script Roadmap

Public roadmap. Vote on items via 👍 reactions on the linked issues.

> Founded by [Jassim Alkharafi](https://github.com/jlkdevelop). MX Script is MIT-licensed and built in the open.

---

## Shipped

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

## Next up (v0.3 candidates)

These are the things we'd most like help with.

- [ ] **Pre-built binaries** for macOS / Linux / Windows on every tag (GoReleaser config exists; verify on first tag).
- [ ] **`mx fmt`** — opinionated formatter for `.mx` files.
- [ ] **WebSocket routes** — `route WS /chat { ... }`.
- [ ] **SQLite driver** — `db.query("select ...")` as a built-in.
- [ ] **Cookies** — `request.cookies` and `set_cookie(name, value)`.
- [ ] **Static file serving** — `static "./public"` directive.
- [ ] **Spread operator** — `let combined = [...a, ...b]`.

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
