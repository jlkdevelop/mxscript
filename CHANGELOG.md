# Changelog

All notable changes to MX Script are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/).

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
