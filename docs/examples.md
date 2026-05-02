# Examples

Five real-world patterns in MX Script.

## 1. Health check + version endpoint

```mx
server { port: 8080 }

route GET /health {
  return json({
    status: "ok",
    version: "1.0.0",
    uptime_ms: now()
  })
}
```

## 2. CRUD with in-memory store

```mx
let posts = []
let nextId = 1

route GET /posts {
  return json(posts)
}

route POST /posts {
  let body = request.body
  if (body == null || body.title == null) {
    return status(400, { error: "title required" })
  }
  let post = { id: nextId, title: body.title }
  posts = push(posts, post)
  nextId = nextId + 1
  return status(201, post)
}

route GET /posts/:id {
  let id = num(request.params.id)
  let post = find(posts, fn(p) { return p.id == id })
  if (post == null) {
    return status(404, { error: "not found" })
  }
  return json(post)
}

route DELETE /posts/:id {
  let id = num(request.params.id)
  posts = filter(posts, fn(p) { return p.id != id })
  return json({ deleted: id })
}
```

## 3. Auth middleware

```mx
middleware require_token {
  if (request.headers["authorization"] != "Bearer " + env("API_TOKEN")) {
    return status(401, { error: "unauthorized" })
  }
}

route GET /private {
  use require_token
  return json({ secret: "hello, authenticated user" })
}
```

## 4. Calling another API with `fetch`

```mx
route GET /github/:user {
  let res = fetch("https://api.github.com/users/" + request.params.user)
  if (res.status >= 400) {
    return status(res.status, { error: "github says no" })
  }
  return json({
    name: res.body.name,
    repos: res.body.public_repos,
    followers: res.body.followers
  })
}
```

## 5. AI-powered summariser

```mx
route POST /summarise {
  let body = request.body
  if (body == null || body.text == null) {
    return status(400, { error: "text required" })
  }
  try {
    let summary = ai.complete(
      "Summarise the following in two sentences:\n\n" + body.text
    )
    return json({ summary: summary })
  } catch (e) {
    return status(500, { error: e.message })
  }
}
```

Set `OPENAI_API_KEY` in the environment, hit the endpoint, get a summary back.
