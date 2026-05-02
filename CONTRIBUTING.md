# Contributing to MX Script

First — thank you. Open source projects only exist because people show up. Whether you're fixing a typo, filing a bug, or shipping a whole new feature, we're glad you're here.

MX Script is founded and maintained by [Jassim Alkharafi](https://github.com/jlkdevelop). The project is MIT-licensed and entirely open: no CLA, no contributor agreements, no waiting list.

---

## Quick links

- 🐛 [Report a bug](https://github.com/jlkdevelop/mxscript/issues/new?template=bug.md)
- 💡 [Request a feature](https://github.com/jlkdevelop/mxscript/issues/new?template=feature.md)
- 💬 [Discussions](https://github.com/jlkdevelop/mxscript/discussions)
- 📖 [Docs](docs/)

---

## Development setup

You need Go 1.21+ installed. That's it — MX Script has zero external runtime dependencies.

```bash
git clone https://github.com/jlkdevelop/mxscript.git
cd mxscript
go build -o mx .
go test ./...
./mx run examples/app.mx
```

That's the entire dev loop. No package manager. No virtualenv. No Docker.

---

## Workflow

### 1. Fork & clone

Fork [jlkdevelop/mxscript](https://github.com/jlkdevelop/mxscript) on GitHub, then:

```bash
git clone https://github.com/YOUR-USERNAME/mxscript.git
cd mxscript
git remote add upstream https://github.com/jlkdevelop/mxscript.git
```

### 2. Branch

```bash
git checkout -b feat/your-feature
```

Branch naming conventions (loose):

- `feat/<name>` — new features
- `fix/<name>` — bug fixes
- `docs/<name>` — docs only
- `refactor/<name>` — code cleanup, no behavior change

### 3. Build & test

```bash
go build -o mx .
go test ./...
```

Run an example to verify your change end-to-end:

```bash
./mx run examples/app.mx
curl http://localhost:8080/users
```

### 4. Format

```bash
gofmt -w .
```

CI rejects unformatted code.

### 5. Commit

Follow [Conventional Commits](https://www.conventionalcommits.org/) where possible:

```
feat(parser): support template literals
fix(interpreter): correct precedence of !=
docs: clarify middleware example
```

### 6. Push & open a PR

```bash
git push origin feat/your-feature
```

Then open a PR against `main`. Use the PR template — it asks three questions:

1. What does this change?
2. Why?
3. How did you test it?

---

## What to work on

If you're looking for somewhere to start:

- 🟢 [Good first issues](https://github.com/jlkdevelop/mxscript/issues?q=is%3Aopen+label%3A%22good+first+issue%22)
- 🟡 [Help wanted](https://github.com/jlkdevelop/mxscript/issues?q=is%3Aopen+label%3A%22help+wanted%22)
- 🔴 The [roadmap](ROADMAP.md) lists big-picture items

The fastest path to landing your first PR: pick a built-in function on the roadmap that doesn't exist yet, implement it in `interpreter/builtins.go`, write a test, document it in `docs/api.md`, open a PR.

---

## Code style

- Run `gofmt -w .` before committing.
- Keep new files short and focused. The interpreter is intentionally one file you can read in an evening.
- Every Go file has a comment header explaining what it does.
- Avoid adding external Go dependencies. The standard library is the standard library.
- Keep error messages user-friendly: include line numbers, file paths, and what was expected.

---

## Tests

MX Script tests live alongside the code:

- `lexer/lexer_test.go` — token boundaries, edge cases
- `parser/parser_test.go` — AST shape, error recovery
- `interpreter/interpreter_test.go` — end-to-end programs

Run them all:

```bash
go test ./...
```

A new feature should land with at least one test that exercises the happy path and one that exercises an error.

---

## Reporting bugs

A great bug report has:

1. The smallest `.mx` program that reproduces the bug.
2. What you expected.
3. What actually happened (full output, not paraphrased).
4. Your `mx version`.

Use the [bug template](https://github.com/jlkdevelop/mxscript/issues/new?template=bug.md) — it asks for exactly these things.

---

## Code of conduct

Be kind. Assume good faith. Disagree on technical merit, never on the person.

If something feels off, email the maintainer directly: jlkdevelop@gmail.com.

---

## License

By contributing to MX Script you agree your contributions will be licensed under the [MIT License](LICENSE), the same license that covers the rest of the project.

— Jassim Alkharafi, founder
