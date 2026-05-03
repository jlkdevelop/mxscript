# Examples

Five common patterns in MX Script. Copy-paste, change the bits you don't like, ship.

## 1. Validated POST + Insert

```mx
let SCHEMA = {
  type: "object",
  required: ["name", "email"],
  properties: {
    name:  { type: "string", min_length: 2 },
    email: { type: "string", format: "email" }
  }
}

post /users {
  let r = body_validate(request, SCHEMA)
  if (!r.ok) { return r.response }                   // 400 problem+json with errors

  let id = sql.insert(db, "users", r.body).last_insert_id
  return status(201, { id: id })
}
```

`body_validate()` returns either a validated body or a fully-formed `application/problem+json` 400 — no manual error envelope construction.

## 2. Paginated list

```mx
get /users {
  let p     = paginate(request)
  let total = sql.count(db, "users", {})
  let items = sql.find(db, "users", {}, {
    order:  "id DESC",
    limit:  p.limit,
    offset: p.offset
  })
  return json(page_response(items, p, total))
}
```

`paginate(request)` reads `?page=` and `?per_page=` with sensible defaults. `page_response()` builds the standard `{ items, page, per_page, total, total_pages, has_next, has_prev }` envelope.

## 3. Cacheable detail endpoint with ETag

```mx
get /users/:id {
  let user = sql.find_one(db, "users", { id: num(request.params.id) })
  if (user == null) { return problem(404, "User not found") }

  let tag = etag(user)
  if (request.headers["if-none-match"] == tag) { return not_modified() }

  return json(user, {
    headers: {
      "ETag":          tag,
      "Cache-Control": cache_control({ private: true, max_age: 60 })
    }
  })
}
```

First request pays for the JSON body once. Subsequent identical requests get 304s with no body.

## 4. File upload

```mx
post /users/:id/avatar {
  let img = request.files?.image
  if (img == null)         { return problem(400, "Missing 'image' file") }
  if (img.size > 5_000_000) { return problem(413, "Max 5 MB") }

  let saved = save_upload(img, "./uploads/" + uuid() + img.ext)
  if (!saved.ok) { return problem(500, "Save failed", saved.error) }

  return json({ url: saved.path, size: saved.size })
}
```

`request.files` is auto-parsed from `multipart/form-data`. `save_upload` writes atomically and creates parent dirs.

## 5. Service-to-service auth

```mx
middleware require_api_key {
  if (!api_key_auth(request, env("API_KEYS"))) {
    return problem(401, "Invalid API key")
  }
}

group /api {
  use require_api_key
  get /me { return json({ ok: true }) }
}
```

`api_key_auth` checks `X-API-Key` (falls back to `Authorization: Bearer`). Allow-list comes from a comma-separated env var. Constant-time compare. Empty allow-list always returns false (fail-closed).

## More

- `mx new shortener` — a complete URL shortener in 50 lines
- `mx new api` — REST API showcase
- `mx new todo` — JWT + SQLite
- `mx new ai` — tool-calling agent endpoint
- `examples/` in the source tree — runnable demos
