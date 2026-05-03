// apikey.go — service-to-service auth helper.
//
// Pattern:
//
//	middleware require_api_key {
//	  if (!api_key_auth(request, env("API_KEYS"))) {
//	    return status(401, { error: "invalid api key" })
//	  }
//	}
//
//	group /api {
//	  use require_api_key
//	  get /health { return json({ ok: true }) }
//	}
//
// Reads `X-API-Key` first, falls back to `Authorization: Bearer <key>`.
// `allowed_keys` is a comma-separated string of valid keys, typically
// from env("API_KEYS"). Compare is constant-time so timing leaks
// can't enumerate keys one byte at a time.
package interpreter

import (
	"crypto/subtle"
	"fmt"
	"strings"
)

// api_key_auth(request, allowed_keys) -> bool
//
// `allowed_keys` is a string. Multiple keys are comma-separated.
// An empty `allowed_keys` always returns false — fail-closed if the
// env var is unset, so a misconfigured deploy doesn't accept anything.
func builtinAPIKeyAuth(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("api_key_auth(request, allowed_keys) requires 2 arguments")
	}
	if args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("api_key_auth: first arg must be the request object")
	}
	allowedRaw, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if strings.TrimSpace(allowedRaw) == "" {
		return BoolValue(false), nil
	}
	presented := extractAPIKey(args[0].Object)
	if presented == "" {
		return BoolValue(false), nil
	}
	for _, k := range strings.Split(allowedRaw, ",") {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(k)) == 1 {
			return BoolValue(true), nil
		}
	}
	return BoolValue(false), nil
}

// extractAPIKey looks at request.headers for X-API-Key first; falls
// back to bearer_token. Returns "" if neither is present.
func extractAPIKey(req *OrderedMap) string {
	if h, ok := req.Get("headers"); ok && h.Kind == KindObject {
		// Header lookup is case-sensitive on the MX side because we
		// store them lowercased upstream — try both common cases so
		// users who stuff custom headers in tests still get the right
		// hit.
		for _, key := range []string{"x-api-key", "X-API-Key", "x_api_key"} {
			if v, ok := h.Object.Get(key); ok && v.Kind == KindString && v.String != "" {
				return v.String
			}
		}
	}
	if v, ok := req.Get("bearer_token"); ok && v.Kind == KindString && v.String != "" {
		return v.String
	}
	return ""
}
