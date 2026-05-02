# API reference

Every built-in function in MX Script.

## Output

### `print(...args)`
### `println(...args)`
Print arguments to stdout, space-separated, with a trailing newline.

```mx
print("Hello,", "world")     // Hello, world
print({ a: 1 })              // {"a":1}
```

## HTTP responses

### `json(value)`
Wraps `value` as a JSON response (`Content-Type: application/json`).

### `text(value)`
Returns a plain text response.

### `html(value)`
Returns an HTML response.

### `status(code, body?)`
Sets the HTTP status. Body defaults to JSON if not a string.

```mx
return status(404, { error: "not found" })
```

### `redirect(url, code?)`
302 redirect (or whatever code you pass).

## HTTP requests

### `fetch(url, opts?)`
Outbound HTTP request. `opts` is an optional object:

| Key        | Type    | Default |
|------------|---------|---------|
| `method`   | string  | `"GET"` |
| `body`     | any     | none    |
| `headers`  | object  | none    |

If `body` is an object/array it's JSON-encoded automatically.

Returns an object: `{ status, headers, body, text }`. `body` is auto-decoded JSON when applicable.

```mx
let res = fetch("https://api.github.com/users/jlkdevelop")
print(res.body.name)
```

## Environment

### `env(name, default?)`
Reads an environment variable. Returns the default if unset.

```mx
let port = num(env("PORT", "8080"))
```

## Strings

| Function                    | Description                                |
|-----------------------------|--------------------------------------------|
| `len(s)`                    | Character count                            |
| `upper(s)`                  | Uppercase                                  |
| `lower(s)`                  | Lowercase                                  |
| `trim(s)`                   | Remove leading/trailing whitespace         |
| `split(s, sep)`             | Split into array                           |
| `contains(s, sub)`          | Substring check (also works on arrays)     |
| `replace(s, old, new)`      | Replace all occurrences                    |
| `starts_with(s, prefix)`    | Boolean prefix check                       |
| `ends_with(s, suffix)`      | Boolean suffix check                       |
| `str(x)`                    | Coerce to string                           |
| `num(s)`                    | Parse to number                            |

## Arrays

| Function              | Description                                |
|-----------------------|--------------------------------------------|
| `len(a)`              | Element count                              |
| `push(a, ...vals)`    | Returns a new array with values appended   |
| `pop(a)`              | Returns the last element (or null)         |
| `map(a, fn)`          | Transform each element                     |
| `filter(a, fn)`       | Keep elements where fn returns truthy      |
| `find(a, fn)`         | First element matching fn (or null)        |
| `join(a, sep?)`       | Concatenate as string                      |
| `reverse(a)`          | Reversed copy                              |
| `range(end)`          | `[0, 1, ..., end-1]`                       |
| `range(start, end)`   | Inclusive start, exclusive end             |
| `contains(a, val)`    | Membership check                           |

## Objects

| Function          | Description                                    |
|-------------------|------------------------------------------------|
| `len(o)`          | Number of keys                                 |
| `keys(o)`         | Array of keys (insertion order)                |
| `values(o)`       | Array of values                                |

## Math

| Function              | Description                                |
|-----------------------|--------------------------------------------|
| `round(n)`            | Nearest integer                            |
| `floor(n)`            | Round toward -infinity                     |
| `ceil(n)`             | Round toward +infinity                     |
| `abs(n)`              | Absolute value                             |
| `min(...nums)`        | Smallest                                   |
| `max(...nums)`        | Largest                                    |
| `random()`            | Float in [0, 1)                            |
| `random(n)`           | Integer in [0, n)                          |
| `random(lo, hi)`      | Integer in [lo, hi)                        |

## Types

| Function              | Returns                                    |
|-----------------------|--------------------------------------------|
| `typeof(x)`           | `"null"`, `"bool"`, `"number"`, `"string"`, `"array"`, `"object"`, `"function"` |
| `isString(x)`         | bool                                       |
| `isNumber(x)`         | bool                                       |
| `isBool(x)`           | bool                                       |
| `isNull(x)`           | bool                                       |
| `isArray(x)`          | bool                                       |
| `isObject(x)`         | bool                                       |
| `isFunction(x)`       | bool                                       |

## JSON

### `json_parse(s)`
Parse a JSON string into an MX value.

### `json_stringify(v)`
Serialize a value to a JSON string.

## File I/O

### `read_file(path)`
Returns the file contents as a string.

### `write_file(path, content)`
Writes a string to disk. Creates the file if it doesn't exist; overwrites otherwise.

### `file_exists(path)`
Boolean check.

### `list_files(dir)`
Returns an array of paths in `dir`.

### `delete_file(path)`
Removes the file at `path`.

## Crypto / encoding

### `hash_sha256(s)`
Returns the SHA-256 hex digest of a string.

### `base64_encode(s)` / `base64_decode(s)`
Standard base64 encode and decode.

### `uuid()`
Returns an RFC 4122 v4 UUID using `crypto/rand`.

## Time

### `now()`
Current Unix time in milliseconds.

### `now_iso()`
Current UTC time as an RFC 3339 / ISO 8601 string.

### `sleep(ms)`
Block for `ms` milliseconds.

## Errors

### `error(msg)`
Throw a runtime error. Catch with `try` / `catch`.

```mx
try {
  if (request.body.amount < 0) {
    error("amount must be non-negative")
  }
} catch (e) {
  return status(400, { error: e.message })
}
```

## AI

### `ai.complete(prompt, opts?)`

Calls an OpenAI-compatible chat API. Requires `OPENAI_API_KEY` in the environment.

`opts` (optional):

| Key          | Default          |
|--------------|------------------|
| `model`      | `gpt-4o-mini`    |
| `max_tokens` | `256`            |

```mx
let answer = ai.complete("In one sentence, what is MX Script?")
print(answer)
```

### `ai.embed(text)`

Returns a `text-embedding-3-small` vector as an array of floats.

```mx
let vec = ai.embed("hello world")
print(len(vec))   // 1536
```
