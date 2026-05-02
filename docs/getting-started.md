# Getting started with MX Script

Five minutes from zero to a running JSON API.

## Install

You need [Go 1.21+](https://go.dev/dl/). Then:

```bash
git clone https://github.com/jlkdevelop/mxscript.git
cd mxscript
go build -o mx .
```

Verify:

```bash
./mx version
# MX Script v0.1.0
```

Drop `mx` somewhere on your `$PATH` (e.g. `/usr/local/bin/mx`) so you can call it from anywhere.

## Your first program

Make a file called `hello.mx`:

```mx
print("Hello, MX Script!")
```

Run it:

```bash
mx run hello.mx
```

That's it. No project file, no `main()`, no config.

## Your first API

Make `app.mx`:

```mx
server {
  port: 8080
}

route GET / {
  return json({ message: "hello world" })
}

route GET /users/:id {
  return json({ id: request.params.id })
}
```

Run it:

```bash
mx run app.mx
```

You'll see:

```
🚀 MX Script running at http://localhost:8080

Routes:
  GET    /
  GET    /users/:id
```

In another terminal:

```bash
curl http://localhost:8080/
# {"message":"hello world"}

curl http://localhost:8080/users/42
# {"id":"42"}
```

## Hot reload

Pass `--watch` and `mx` will restart automatically whenever any `.mx` file in the directory changes:

```bash
mx run app.mx --watch
```

## Scaffolding

`mx init my-api` creates a starter project so you don't have to remember the boilerplate:

```bash
mx init my-api
cd my-api
mx run app.mx
```

## Next steps

- [Syntax reference](syntax.md) — the full language
- [API reference](api.md) — every built-in function
- [Examples](examples.md) — common patterns
