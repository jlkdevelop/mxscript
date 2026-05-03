// ids_objects.go — ID generators (uuid/ulid/nanoid/short) and a small
// suite of object helpers (pick/omit/merge/deep_merge). All are
// expression-friendly and copy-rather-than-mutate so chained
// transformations don't surprise callers.
package interpreter

import (
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// ===== id.* =====

// crockfordBase32 is the alphabet ULID and Crockford-style ids use.
// No vowels (avoids accidental words), no I/L/O (avoids 1/0 confusion).
const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// nanoidAlphabet is the URL-safe alphabet nanoid uses by default —
// no underscores, no padding, deterministically URL-safe.
const nanoidAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// id.uuid() — RFC 4122 v4 (alias for the existing top-level uuid()).
// We re-expose under id.* so apps that lean on the namespace pattern
// don't have to mix top-level + namespace calls.
func builtinIDUUID(i *Interpreter, args []Value) (Value, error) {
	return builtinUUID(i, args)
}

// id.ulid() — Crockford-base32, 26 chars, sorts lexicographically by
// time. Spec: https://github.com/ulid/spec
//
// First 10 chars are the millisecond timestamp; last 16 are random.
// Two ULIDs minted in the same millisecond differ in the random
// portion only.
func builtinIDULID(_ *Interpreter, _ []Value) (Value, error) {
	now := uint64(time.Now().UnixMilli())
	var entropy [10]byte
	if _, err := crand.Read(entropy[:]); err != nil {
		return Value{}, err
	}
	out := make([]byte, 26)

	// Encode the 48-bit timestamp into the first 10 chars (5 bits each).
	for i := 9; i >= 0; i-- {
		out[i] = crockfordBase32[now&0x1F]
		now >>= 5
	}
	// Encode the 80 bits of entropy into the last 16 chars.
	bits := uint64(0)
	bitCount := 0
	idx := 10
	for _, b := range entropy {
		bits = (bits << 8) | uint64(b)
		bitCount += 8
		for bitCount >= 5 {
			bitCount -= 5
			out[idx] = crockfordBase32[(bits>>uint(bitCount))&0x1F]
			idx++
			if idx >= 26 {
				break
			}
		}
		if idx >= 26 {
			break
		}
	}
	return StringValue(string(out)), nil
}

// id.nanoid(n?) — URL-safe random string. Defaults to 21 chars
// (~3.4×10^36 unique values, the nanoid spec default).
func builtinIDNanoID(_ *Interpreter, args []Value) (Value, error) {
	n := 21
	if len(args) > 0 && args[0].Kind == KindNumber {
		n = int(args[0].Number)
	}
	if n <= 0 {
		return StringValue(""), nil
	}
	buf := make([]byte, n)
	if _, err := crand.Read(buf); err != nil {
		return Value{}, err
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = nanoidAlphabet[int(buf[i])%len(nanoidAlphabet)]
	}
	return StringValue(string(out)), nil
}

// id.short() — 8-char URL-safe ID. Convenient for invitation codes,
// share tokens, and anything else that should fit in a URL without
// looking ugly.
func builtinIDShort(i *Interpreter, _ []Value) (Value, error) {
	return builtinIDNanoID(i, []Value{NumberValue(8)})
}

// id.snowflake(epoch?) — 64-bit time-sortable ID, packed as a
// number-string (since MX numbers are float64 and would lose precision
// on 64-bit ints). 41 bits ms-since-epoch + 22 bits randomness.
//
//	epoch defaults to 2020-01-01T00:00:00Z so the first ~70 years fit
//	in 41 bits.
func builtinIDSnowflake(_ *Interpreter, args []Value) (Value, error) {
	epoch := int64(1577836800000) // 2020-01-01 UTC, ms
	if len(args) > 0 && args[0].Kind == KindNumber {
		epoch = int64(args[0].Number)
	}
	now := time.Now().UnixMilli() - epoch
	if now < 0 {
		now = 0
	}
	var rnd [3]byte
	if _, err := crand.Read(rnd[:]); err != nil {
		return Value{}, err
	}
	rndBits := uint64(rnd[0])<<14 | uint64(rnd[1])<<6 | uint64(rnd[2]>>2)
	id := (uint64(now) << 22) | (rndBits & 0x3FFFFF)
	return StringValue(fmt.Sprintf("%d", id)), nil
}

func init() {
	// Silence unused-import warnings when building with stripped tags.
	_ = binary.BigEndian
	_ = strings.Builder{}
}

// ===== Object helpers =====

// pick(obj, keys) — return a new object containing only the named keys.
//
//	let safe = pick(user, ["id", "email", "name"])
func builtinPick(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindObject || args[1].Kind != KindArray {
		return Value{}, fmt.Errorf("pick(obj, keys) requires (object, array)")
	}
	out := NewOrderedMap()
	for _, k := range args[1].Array {
		if k.Kind != KindString {
			continue
		}
		if v, ok := args[0].Object.Get(k.String); ok {
			out.Set(k.String, v)
		}
	}
	return ObjectValue(out), nil
}

// omit(obj, keys) — return a new object with the named keys removed.
//
//	let public = omit(user, ["password_hash", "api_key"])
func builtinOmit(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindObject || args[1].Kind != KindArray {
		return Value{}, fmt.Errorf("omit(obj, keys) requires (object, array)")
	}
	skip := map[string]bool{}
	for _, k := range args[1].Array {
		if k.Kind == KindString {
			skip[k.String] = true
		}
	}
	out := NewOrderedMap()
	for _, k := range args[0].Object.Keys {
		if skip[k] {
			continue
		}
		v, _ := args[0].Object.Get(k)
		out.Set(k, v)
	}
	return ObjectValue(out), nil
}

// merge(a, b) — shallow merge; b's values overwrite a's on key clash.
// Existing key order from a is preserved; new keys from b append.
func builtinMerge(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindObject || args[1].Kind != KindObject {
		return Value{}, fmt.Errorf("merge(a, b) requires two objects")
	}
	out := NewOrderedMap()
	for _, k := range args[0].Object.Keys {
		v, _ := args[0].Object.Get(k)
		out.Set(k, v)
	}
	for _, k := range args[1].Object.Keys {
		v, _ := args[1].Object.Get(k)
		out.Set(k, v)
	}
	return ObjectValue(out), nil
}

// deep_merge(a, b) — recursive merge. When both sides have an object
// at the same key, descend; otherwise b's value wins.
//
//	let cfg = deep_merge(defaults, user_overrides)
func builtinDeepMerge(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindObject || args[1].Kind != KindObject {
		return Value{}, fmt.Errorf("deep_merge(a, b) requires two objects")
	}
	return ObjectValue(deepMerge(args[0].Object, args[1].Object)), nil
}

func deepMerge(a, b *OrderedMap) *OrderedMap {
	out := NewOrderedMap()
	for _, k := range a.Keys {
		v, _ := a.Get(k)
		out.Set(k, v)
	}
	for _, k := range b.Keys {
		bv, _ := b.Get(k)
		if av, ok := out.Get(k); ok && av.Kind == KindObject && bv.Kind == KindObject {
			out.Set(k, ObjectValue(deepMerge(av.Object, bv.Object)))
		} else {
			out.Set(k, bv)
		}
	}
	return out
}
