// config.go — convenience config loader with format auto-detection
// and env-var interpolation. Common SaaS pattern:
//
//   # config.yaml
//   db:
//     dsn: ${DATABASE_URL}
//     pool: 10
//   stripe:
//     secret: ${STRIPE_SECRET_KEY}
//     price:  ${STRIPE_PRICE_ID}
//
//   # app.mx
//   let cfg = config.load("./config.yaml")
//   let db  = sql.open(cfg.db.dsn)
//
// Supports .yaml / .yml / .json / .toml — picked by extension.
// `${NAME}` placeholders expand to env(NAME) before parsing, so a
// committed config file can carry secret references without the
// secrets themselves.
package interpreter

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// envSubPattern matches ${NAME} and ${NAME:-default} in raw config
// text. Multi-letter, allowing underscores and digits after the
// first char.
var envSubPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// config.load(path) — read + parse a config file by extension.
// `${NAME}` and `${NAME:-default}` references in the raw file
// expand from os.Getenv before parsing.
func builtinConfigLoad(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Value{}, err
	}
	expanded := envSubPattern.ReplaceAllStringFunc(string(raw), func(match string) string {
		sub := envSubPattern.FindStringSubmatch(match)
		name := sub[1]
		def := sub[2]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return def
	})

	switch {
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
		return builtinYAMLParse(i, []Value{StringValue(expanded)})
	case strings.HasSuffix(path, ".json"):
		// Reuse the existing json_parse builtin via the public hook.
		return builtinJSONParse(i, []Value{StringValue(expanded)})
	case strings.HasSuffix(path, ".toml"):
		return builtinTOMLParse(i, []Value{StringValue(expanded)})
	default:
		return Value{}, fmt.Errorf("config.load: unknown extension on %q (want .yaml / .json / .toml)", path)
	}
}

// config.expand(s) — expand ${NAME} placeholders in any string.
// Useful for templating connection strings or filenames at runtime.
func builtinConfigExpand(_ *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	expanded := envSubPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := envSubPattern.FindStringSubmatch(match)
		name := sub[1]
		def := sub[2]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return def
	})
	return StringValue(expanded), nil
}
