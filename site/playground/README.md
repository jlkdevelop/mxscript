# MX Script Playground

A single-page browser playground that runs MX Script programs entirely client-side via the interpreter compiled to WebAssembly. No server, no API — your code stays on your machine.

## Files

| File | Source | Notes |
|---|---|---|
| `index.html` | committed | the playground UI |
| `wasm_exec.js` | committed | Go's standard JS host shim |
| `mx.wasm` | **gitignored** — built by `mx build --wasm` | the interpreter (~14 MB) |

## Build the wasm bundle

```bash
mx build --wasm                       # produces dist/mx.wasm + dist/wasm_exec.js
cp dist/mx.wasm site/playground/      # drop it next to index.html
```

Or in one shot:

```bash
mx build --wasm && cp dist/mx.wasm site/playground/
```

## Serve locally

Any static-file server works. Two easy options:

```bash
# Python
cd site/playground && python3 -m http.server 8085
# then open http://localhost:8085

# Go
cd site/playground && go run github.com/jpillora/serve@latest -p 8085
```

The wasm runtime requires the page to be served over HTTP — opening `index.html` directly with `file://` won't work because `WebAssembly.instantiateStreaming` rejects non-http origins.

## Deploy

Anywhere that hosts static files: GitHub Pages, Vercel, Netlify, S3, Cloudflare Pages. Just upload the three files together. Set the appropriate `Content-Type: application/wasm` MIME for `.wasm` if your host doesn't already.

## What works in the browser

Everything that doesn't need raw TCP, file system access, or process spawning. So:

- ✅ All language features: routes parse but won't serve, every operator, every literal, control flow, closures, destructuring, match, try/catch, modules, the bytecode VM
- ✅ Pure-data builtins: `json_*`, `map`/`filter`/`reduce`/`sort`, regex, `random_*`, time, math, string helpers, base32/base64, hashing, JWT, magic_link, TOTP, templates
- ❌ SQL (no SQLite in the browser)
- ❌ Redis (no raw TCP)
- ❌ Background jobs (no SQLite)
- ❌ HTTP server (the page can't bind ports)
- ⚠ `fetch()` — works but subject to CORS

For the full server experience, run `mx run app.mx` natively.
