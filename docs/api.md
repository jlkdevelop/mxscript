# API reference — cheat sheet

The complete surface lives in `mx help` and `mx help <name>`. This is
the working set for one-file web APIs — what you'll actually reach for.

## Routing

```mx
get /path { ... }
post /users/:id { ... }
group /api { use auth; get /me { ... } }
ws /chat { ... }       // WebSocket
sse /events { ... }    // server-sent events
```

`request` is in scope inside every handler:

| Field | Type | Notes |
|---|---|---|
| `request.method` | string | `"GET"`, `"POST"`, ... |
| `request.path` | string | `/users/42` |
| `request.params` | object | path params from `:id` etc. |
| `request.query` | object | parsed query string |
| `request.headers` | object | lowercased keys |
| `request.body` | any | auto-parsed JSON / form / multipart |
| `request.files` | object | multipart files keyed by field name |
| `request.cookies` | object | parsed cookies |
| `request.bearer_token` | string | `Authorization: Bearer ...` extracted |
| `request.is_json` | bool | `Content-Type` is `application/json` |
| `request.ip` | string | client IP (honors `X-Forwarded-For`) |
| `request.id` | string | trace ID (auto-generated or honors `X-Request-ID`) |

## Responses

```mx
return json(value, opts?)        // application/json
return text(string, opts?)       // text/plain
return html(string, opts?)       // text/html
return status(code, body?, opts?)
return redirect(url, code?)
return not_modified()            // 304
return problem(status, title, detail?, ext?)  // RFC 7807 application/problem+json
return csv(items, opts?)         // text/csv  (opts.filename = download)
return ndjson(items)             // application/x-ndjson
return static_file(path)         // serve a file from disk
return proxy("http://upstream", request)
```

`opts` carries `headers`, `cookies`, etc.

## API helpers

```mx
let p = paginate(request)                    // { page, per_page, limit, offset }
let r = page_response(items, p, total)       // standard list envelope

let v = body_validate(request, schema)       // { ok: true, body } | { ok: false, response }
return problem(400, "Validation failed", "", { errors: [...] })

let tag = etag(value)
if (request.headers["if-none-match"] == tag) { return not_modified() }
return json(value, { headers: { "ETag": tag, "Cache-Control": cache_control({ public: true, max_age: 300 }) } })

let img = request.files?.image
let saved = save_upload(img, "./uploads/" + uuid() + img.ext)

if (!api_key_auth(request, env("API_KEYS"))) { return status(401, ...) }
```

## Object-driven SQL

```mx
let db = sql.open("./app.db")          // sqlite — also "postgres://..." or "mysql://..."
sql.migrate(db, ["CREATE TABLE ..."])

sql.find(db, "users", { active: 1 }, { order: "id DESC", limit: 10 })
sql.find_one(db, "users", { id: 1 })
sql.count(db, "users", { role: "admin" })
sql.exists(db, "users", { email: e })
sql.insert(db, "users", { name: "Ada" })                       // single
sql.insert(db, "users", [{ ... }, { ... }])                    // batched
sql.upsert(db, "users", { id: 1, ... }, ["id"])                // ON CONFLICT
sql.update(db, "users", { active: 0 }, { id: 1 })
sql.delete(db, "users", { id: 1 })

// Drop down to raw when needed:
sql.exec(db, "UPDATE ...", args)
sql.query(db, "SELECT ...", args)
sql.transaction(db, fn(tx) { ... })
```

## Auth

```mx
// JWT
let token = jwt.sign({ sub: user.id, exp: now()/1000 + 3600 }, env("JWT_SECRET"))
let claims = jwt.verify(request.bearer_token, env("JWT_SECRET"))

// Cookie sessions
return session.create({ user_id: 1 }, { secret: SECRET, max_age: 86400 })
let claims = session.read(request, SECRET)
return session.destroy()

// Service-to-service
if (!api_key_auth(request, env("API_KEYS"))) { return problem(401, ...) }

// OAuth (Google / GitHub / Discord / LinkedIn / Microsoft)
oauth.authorize_url({ provider: "google" })
oauth.exchange({ provider: "google", code: code })

// Passwordless
magic_link.send(email)
totp.verify(code, secret)

// Webhook signature verification
webhooks.verify_stripe(body, sig, secret)
webhooks.verify_github(body, sig, secret)
```

## AI

```mx
// 13 providers + custom — provider: "openai" (default) or "anthropic" / "gemini"
// / "groq" / "mistral" / "together" / "openrouter" / "ollama" / "perplexity" /
// "fireworks" / "cerebras" / "deepseek" / "grok" / "custom"
ai.complete(prompt, { provider: "anthropic", max_tokens: 200 })
ai.stream(prompt, fn(chunk) { ... })
ai.embed(text)
ai.vision(prompt, [image_bytes_or_url])

// Tool calling
ai.complete("", {
  messages: [{ role: "user", content: "What time is it?" }],
  tools: [{ name: "now", description: "...", params: {...}, handler: fn(_) { return now_iso() } }]
})

// Custom OpenAI-compatible endpoint
ai.complete("hi", { provider: "custom", base_url: "https://my-vllm/v1/chat/completions", api_key_env: "VLLM_KEY" })
```

## Common patterns

```mx
// Pagination
let p     = paginate(request)
let total = sql.count(db, "users", {})
let items = sql.find(db, "users", {}, { order: "id DESC", limit: p.limit, offset: p.offset })
return json(page_response(items, p, total))

// Validate-then-create
let r = body_validate(request, schema)
if (!r.ok) { return r.response }
let id = sql.insert(db, "users", r.body).last_insert_id
return status(201, { id: id })

// Cached detail
let user = sql.find_one(db, "users", { id: num(request.params.id) })
if (user == null) { return problem(404, "User not found") }
let tag = etag(user)
if (request.headers["if-none-match"] == tag) { return not_modified() }
return json(user, { headers: { "ETag": tag, "Cache-Control": cache_control({ private: true, max_age: 60 }) } })
```

## Beyond this page

`mx help` lists every builtin grouped by namespace. `mx help <name>`
shows the signature + summary for any one. `mx docs <name>` does the
same for any documented user fn (anything with a `///` doc comment).
