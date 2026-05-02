// extra_crypto_config.go — bcrypt-grade password hashing (Argon2id,
// scrypt) plus YAML/TOML parsers. These need external deps that the
// stdlib doesn't ship:
//
//	golang.org/x/crypto/argon2 + scrypt
//	gopkg.in/yaml.v3
//	github.com/BurntSushi/toml
//
// All three are widely-deployed, well-maintained, and pure Go.
package interpreter

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/scrypt"
	"gopkg.in/yaml.v3"
)

// ===== Argon2id =====
//
// Format: $argon2id$v=19$m=<mem>,t=<time>,p=<par>$<salt-b64>$<hash-b64>
// Recommended OWASP defaults: m=64MiB, t=3, p=4, hash=32 bytes.

const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 4
	argonKeyLen  = 32
)

func builtinPasswordHashArgon2(i *Interpreter, args []Value) (Value, error) {
	pw, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	salt, err := randomBytes(16)
	if err != nil {
		return Value{}, err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return StringValue(encoded), nil
}

func builtinPasswordVerifyArgon2(i *Interpreter, args []Value) (Value, error) {
	pw, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	encoded, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return BoolValue(false), nil
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return BoolValue(false), nil
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return BoolValue(false), nil
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return BoolValue(false), nil
	}
	got := argon2.IDKey([]byte(pw), salt, time, memory, threads, uint32(len(want)))
	return BoolValue(subtle.ConstantTimeCompare(got, want) == 1), nil
}

// ===== scrypt =====
//
// Format: $scrypt$N=<n>,r=<r>,p=<p>$<salt-b64>$<hash-b64>
// Defaults: N=2^15, r=8, p=1, hash=32 bytes.

const (
	scryptN      = 1 << 15
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 32
)

func builtinPasswordHashScrypt(i *Interpreter, args []Value) (Value, error) {
	pw, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	salt, err := randomBytes(16)
	if err != nil {
		return Value{}, err
	}
	key, err := scrypt.Key([]byte(pw), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return Value{}, err
	}
	encoded := fmt.Sprintf("$scrypt$N=%d,r=%d,p=%d$%s$%s",
		scryptN, scryptR, scryptP,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return StringValue(encoded), nil
}

func builtinPasswordVerifyScrypt(i *Interpreter, args []Value) (Value, error) {
	pw, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	encoded, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[1] != "scrypt" {
		return BoolValue(false), nil
	}
	var n, r, p int
	for _, kv := range strings.Split(parts[2], ",") {
		eq := strings.SplitN(kv, "=", 2)
		if len(eq) != 2 {
			continue
		}
		v, err := strconv.Atoi(eq[1])
		if err != nil {
			return BoolValue(false), nil
		}
		switch eq[0] {
		case "N":
			n = v
		case "r":
			r = v
		case "p":
			p = v
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return BoolValue(false), nil
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return BoolValue(false), nil
	}
	got, err := scrypt.Key([]byte(pw), salt, n, r, p, len(want))
	if err != nil {
		return BoolValue(false), nil
	}
	return BoolValue(subtle.ConstantTimeCompare(got, want) == 1), nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := crandRead(b)
	return b, err
}

// crandRead is a thin wrapper so this file doesn't need to import crypto/rand
// directly — the existing builtins.go imports it as `crand`.
var crandRead = func(b []byte) (int, error) { return 0, fmt.Errorf("crand not initialised") }

// ===== YAML =====

func builtinYAMLParse(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	var raw any
	if err := yaml.Unmarshal([]byte(s), &raw); err != nil {
		return Value{}, err
	}
	return goAnyToValue(raw), nil
}

func builtinYAMLStringify(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return StringValue(""), nil
	}
	out, err := yaml.Marshal(valueToPlainGo(args[0]))
	if err != nil {
		return Value{}, err
	}
	return StringValue(string(out)), nil
}

// ===== TOML =====

func builtinTOMLParse(i *Interpreter, args []Value) (Value, error) {
	s, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	var raw map[string]any
	if _, err := toml.Decode(s, &raw); err != nil {
		return Value{}, err
	}
	return goAnyToValue(raw), nil
}

func builtinTOMLStringify(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return StringValue(""), nil
	}
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(valueToPlainGo(args[0])); err != nil {
		return Value{}, err
	}
	return StringValue(sb.String()), nil
}

// valueToPlainGo converts an MX Value to a plain Go value tree (no
// json.RawMessage, no preserved-order encoding tricks). Suitable input
// for yaml.Marshal / toml.Encoder, which both handle map / slice / scalar.
func valueToPlainGo(v Value) any {
	switch v.Kind {
	case KindNull:
		return nil
	case KindBool:
		return v.Bool
	case KindNumber:
		// Prefer int64 for whole numbers so YAML/TOML emit `42` not `42.0`.
		if v.Number == float64(int64(v.Number)) {
			return int64(v.Number)
		}
		return v.Number
	case KindString:
		return v.String
	case KindArray:
		out := make([]any, len(v.Array))
		for i, el := range v.Array {
			out[i] = valueToPlainGo(el)
		}
		return out
	case KindObject:
		// YAML/TOML use map[string]any. Order is lost but content is correct.
		out := make(map[string]any, len(v.Object.Keys))
		for _, k := range v.Object.Keys {
			val, _ := v.Object.Get(k)
			out[k] = valueToPlainGo(val)
		}
		return out
	}
	return v.Display()
}

// goAnyToValue converts a freshly-decoded YAML / TOML interface{} tree
// into MX values. Mirrors goToValue but tolerates the wider set of
// types the YAML decoder can produce (map[interface{}]interface{},
// []interface{} of mixed types, etc).
func goAnyToValue(g any) Value {
	switch x := g.(type) {
	case nil:
		return NullValue()
	case bool:
		return BoolValue(x)
	case int:
		return NumberValue(float64(x))
	case int64:
		return NumberValue(float64(x))
	case float64:
		return NumberValue(x)
	case string:
		return StringValue(x)
	case []any:
		out := make([]Value, len(x))
		for i, e := range x {
			out[i] = goAnyToValue(e)
		}
		return ArrayValue(out)
	case map[string]any:
		om := NewOrderedMap()
		for k, v := range x {
			om.Set(k, goAnyToValue(v))
		}
		return ObjectValue(om)
	case map[any]any:
		om := NewOrderedMap()
		for k, v := range x {
			om.Set(fmt.Sprintf("%v", k), goAnyToValue(v))
		}
		return ObjectValue(om)
	}
	return StringValue(fmt.Sprintf("%v", g))
}
