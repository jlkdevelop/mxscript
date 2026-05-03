# Getting started with MX Script

Five minutes from zero to a running, production-shaped JSON API.

## Install

```bash
# macOS or Linux Homebrew
brew install jlkdevelop/tap/mx

# or — one-liner
curl -fsSL https://raw.githubusercontent.com/jlkdevelop/mxscript/main/scripts/install.sh | bash

# or — Go users
go install github.com/jlkdevelop/mxscript@latest
```

Verify:

```bash
mx version
mx help
```

## Your first one-file API

```bash
mx new shortener my-app
cd my-app
mx run app.mx
```

That's a real, deployable URL shortener — paginated stats endpoint,
ETag-cached detail, validated POST, RFC 7807 error responses. Open
`app.mx` to see ~50 lines that exercise the whole MX one-file API
toolkit.

Try it:

```bash
curl -X POST :8080/shorten \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://mxscript.com"}'
# → {"code":"abc1234","short_url":"http://localhost:8080/abc1234",...}

curl -i :8080/abc1234     # 302 redirect, increments hit count
curl :8080/api/links      # paginated list with stats
```

## Templates

```bash
mx new --list             # see all templates with descriptions
mx new api      my-api    # REST showcase: paginate, uploads, OpenAPI, api-key auth
mx new shortener my-short # the canonical 50-line demo
mx new todo     my-todos  # JWT + SQLite todo list
mx new chat     my-chat   # WebSocket chat with embedded browser client
mx new ai       my-bot    # Tool-calling /chat endpoint, 13 LLM providers
mx new blog     my-blog   # SSR markdown blog with admin
mx new saas     my-saas   # Magic-link auth + Stripe + /metrics + cron
mx new dashboard my-admin # Live admin dashboard with charts
mx new react    my-app    # Vite + React + MX backend
```

## What every MX API has

After `mx run app.mx` you automatically get:

| Feature | How to use |
|---|---|
| **Routes** | `get /users/:id { return json(...) }` |
| **JSON in/out** | `request.body` is auto-parsed; `json(value)` responds |
| **Pagination** | `let p = paginate(request); page_response(items, p, total)` |
| **Validation** | `let r = body_validate(request, schema); if (!r.ok) { return r.response }` |
| **Errors** | `return problem(404, "Not found")` — RFC 7807 problem+json |
| **CRUD** | `sql.find/find_one/insert/upsert/update/delete/count/exists` |
| **Caching** | `etag(value)` + `not_modified()` + `cache_control({...})` |
| **Tracing** | `request.id` and `X-Request-ID` are auto-set on every request |
| **Uploads** | `request.files.image`, `save_upload(img, path)` |
| **Auth** | JWT, sessions, OAuth, magic-link, TOTP, `api_key_auth` |
| **Deploy** | `mx build --vercel` / `--docker` / `--fly` / `--railway` / `--compose` |

## Where to look next

- `examples/` — runnable single-file demos
- `examples/url_shortener.mx` — same as `mx new shortener`
- `examples/crud.mx` — REST API for a `posts` collection
- `examples/full_app.mx` — JWT signup + paginated posts + AI summary + SSE feed
- `examples/saas_pro.mx` — Stripe + magic link + AI + S3 + GraphQL + metrics
- `examples/agent.mx` — tool-calling AI agent (CLI mode)
- `examples/chat.mx` — WebSocket chat with embedded browser client
- `mx help <name>` — docs for any builtin (e.g. `mx help sql.find`)
- [`mxscript.com`](https://mxscript.com) — language site
- [GitHub](https://github.com/jlkdevelop/mxscript) — source + releases
