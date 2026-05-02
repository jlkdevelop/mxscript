# Syntax reference

The complete MX Script language in one page.

## Comments

```mx
// line comment
# also a line comment
/* block
   comment */
```

## Literals

```mx
let n      = 42
let pi     = 3.14
let s      = "hello"
let b      = true
let nothing = null
let arr    = [1, 2, 3]
let obj    = { id: 1, name: "Jassim" }
```

Strings support `\n`, `\t`, `\"`, `\\`, `\$` escapes. Numbers are 64-bit floats.

### String interpolation

Strings support `${expr}` template syntax. The expression can be any MX expression — member access, calls, math, anything:

```mx
let name = "Jassim"
let age = 30
print("Hello, ${name}! You are ${age * 2 - 30} years old now.")
print("Upper: ${upper(name)}")

// Escape with backslash to write a literal ${...}
print("\${not interpolated}")
```

## Variables

```mx
let x = 10
x = x + 1     // reassignment is allowed
```

There is no `const` / `var`. Just `let`.

## Operators

| Category   | Operators              |
|------------|------------------------|
| Arithmetic | `+ - * / %`            |
| Comparison | `== != < > <= >=`      |
| Logical    | `&& || !`              |
| String     | `+` (concatenation)    |
| Array      | `+` (concatenation)    |

`+` is overloaded:

```mx
1 + 2          // 3
"hi" + " mx"   // "hi mx"
[1] + [2]      // [1, 2]
"x: " + 42     // "x: 42"   (any operand string → string concat)
```

## Functions

```mx
fn add(a, b) {
  return a + b
}

let double = fn(n) { return n * 2 }   // anonymous function

print(add(2, 3))      // 5
print(double(7))      // 14
```

Functions are first-class values. They close over the enclosing scope.

## Control flow

### if / else / else if

```mx
if (x > 10) {
  print("big")
} else if (x > 0) {
  print("small")
} else {
  print("non-positive")
}
```

Parens around the condition are optional but encouraged.

### loop

`loop <iterable> as <var> { ... }`. Works on arrays, numbers (range), and objects (keys).

```mx
loop [1, 2, 3] as n {
  print(n)
}

loop 5 as i {       // 0, 1, 2, 3, 4
  print(i)
}

loop { a: 1, b: 2 } as key {
  print(key)
}
```

### while

`while (cond) { ... }`. Use this when the iteration count isn't known up front.

```mx
let x = 0
while (x < 100) {
  x = x + 1
  if (x == 50) { break }
}
```

### break and continue

`break` exits the nearest enclosing loop. `continue` skips to the next iteration. Both work inside `loop ... as` and `while`.

```mx
let evens = []
loop 10 as n {
  if (n % 2 != 0) { continue }
  evens = push(evens, n)
}
```

### try / catch

```mx
try {
  let x = num("not a number")
} catch (e) {
  print("oops:", e.message)
}
```

The catch variable is optional: `catch { ... }`.

## Web routes

```mx
server {
  port: 8080
  host: "0.0.0.0"
}

route GET /            { return text("hello") }
route GET /users       { return json([]) }
route GET /users/:id   { return json({ id: request.params.id }) }
route POST /users      { return status(201, request.body) }
route PUT /users/:id   { /* ... */ }
route DELETE /users/:id { /* ... */ }
route PATCH /users/:id  { /* ... */ }
```

### request object

Inside a route body, `request` is auto-injected:

| Field              | Type    | Description                           |
|--------------------|---------|---------------------------------------|
| `request.method`   | string  | `"GET"`, `"POST"`, etc.               |
| `request.path`     | string  | The URL path                          |
| `request.headers`  | object  | Lower-cased header names              |
| `request.query`    | object  | Query string params                   |
| `request.params`   | object  | Path params (`:id`)                   |
| `request.body`     | varies  | Auto-parsed JSON, form, or raw string |

### Response helpers

| Function               | Returns                                 |
|------------------------|-----------------------------------------|
| `json(value)`          | JSON response (`application/json`)      |
| `text(value)`          | Plain text response                     |
| `html(value)`          | HTML response                           |
| `status(code, body)`   | Custom status with body                 |
| `redirect(url, code?)` | 302 redirect (or supplied code)         |

## Middleware

```mx
middleware logger {
  print("[" + request.method + "]", request.path)
}

middleware auth {
  if (request.headers["authorization"] != "Bearer secret") {
    return status(401, { error: "unauthorized" })
  }
}

route GET /admin {
  use logger
  use auth
  return json({ ok: true })
}
```

A middleware that `return`s a value short-circuits the request — the route body is skipped.

## Spread operator

`...expr` expands an array, object, or call argument list inline.

```mx
// Arrays
let combined = [...a, ...b, 7]

// Objects (later keys override earlier ones)
let merged = { ...defaults, ...overrides, debug: true }

// Function calls
fn add4(a, b, c, d) { return a + b + c + d }
print(add4(...[1, 2, 3, 4]))   // 10
```

## Static files

```mx
static "./public"            // serves files at /
static "./assets" at "/cdn"  // serves files at /cdn/...
```

Routes are matched first; static mounts are tried only on no-match. `index.html` is served automatically for directory requests. Path traversal (`..`) returns 403.

## Imports

```mx
import "./utils.mx"

print(my_helper())
```

Imports run the imported file in the current global scope, exposing every top-level `let` and `fn`.

## Idioms

### Default values

```mx
let port = num(env("PORT", "8080"))
```

`env(name, default)` returns the default if the env var is empty.

### Optional bodies

```mx
if (request.body == null) {
  return status(400, { error: "body required" })
}
```
