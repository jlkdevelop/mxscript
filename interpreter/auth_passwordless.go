// auth_passwordless.go — magic-link tokens (signed, time-limited) and
// RFC 6238 TOTP (Google Authenticator–compatible). Together they cover
// the two most common passwordless auth flows. Magic links are great
// for "send me a sign-in email", TOTP is the second factor.
package interpreter

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ===== Magic links =====
//
// A magic-link token is a single base64url-encoded string with three
// parts joined by `.`:
//
//	<email-base64>.<expires-unix>.<hmac-sha256-hex>
//
// The HMAC signs the email + expiry, so a tampered email or expired
// timestamp is rejected by `verify`. The HMAC also makes the token
// stateless — no DB roundtrip needed to authenticate the click.

// MagicLinkCreate builds a signed token for an email that expires in
// `minutes` minutes. Stateless: pass to user, they click, you verify.
func MagicLinkCreate(email, secret string, minutes int) string {
	if minutes <= 0 {
		minutes = 15
	}
	expiresAt := time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
	emailB64 := base64.RawURLEncoding.EncodeToString([]byte(email))
	signedString := emailB64 + "." + strconv.FormatInt(expiresAt, 10)
	mac := hmacSHA256Hex(secret, signedString)
	return signedString + "." + mac
}

// MagicLinkVerify returns the email if `token` is a well-formed,
// untampered, unexpired link signed with `secret`. Otherwise empty.
func MagicLinkVerify(token, secret string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("malformed magic-link token")
	}
	signedString := parts[0] + "." + parts[1]
	expected := hmacSHA256Hex(secret, signedString)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return "", fmt.Errorf("invalid magic-link signature")
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid expiry: %w", err)
	}
	if time.Now().Unix() > expiresAt {
		return "", fmt.Errorf("magic-link expired")
	}
	emailBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid email encoding: %w", err)
	}
	return string(emailBytes), nil
}

func builtinMagicLinkCreate(i *Interpreter, args []Value) (Value, error) {
	email, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	secret, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	minutes := 15
	if len(args) > 2 && args[2].Kind == KindObject {
		if v, ok := args[2].Object.Get("expires_minutes"); ok && v.Kind == KindNumber {
			minutes = int(v.Number)
		}
	}
	return StringValue(MagicLinkCreate(email, secret, minutes)), nil
}

func builtinMagicLinkVerify(i *Interpreter, args []Value) (Value, error) {
	token, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	secret, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	email, vErr := MagicLinkVerify(token, secret)
	if vErr != nil {
		// Returning null lets the route handler stay declarative —
		// `if (email == null) { return status(401, ...) }` reads
		// better than a try/catch around every verify call.
		return NullValue(), nil
	}
	return StringValue(email), nil
}

// ===== TOTP (RFC 6238) =====
//
// We generate a 6-digit code from HMAC-SHA1(secret, counter) where
// counter is `unix-seconds / 30`. Google Authenticator, Authy, 1Password,
// etc. all do the same — the resulting codes are interoperable as long
// as the shared secret is base32-encoded (RFC 4648).

// TOTPGenerate returns the current 6-digit TOTP code for `secret`,
// which must be a base32-encoded string (with or without padding).
func TOTPGenerate(secret string) (string, error) {
	return totpAt(secret, time.Now().Unix()/30)
}

// TOTPVerify accepts the user-supplied `code` if it matches the
// current counter, or any counter within ±drift slots (each slot is
// 30 seconds). Drift defaults to 1 (90-second total window) so a
// slow user / clock skew doesn't lock them out.
func TOTPVerify(code, secret string, drift int) (bool, error) {
	if drift < 0 {
		drift = 0
	}
	now := time.Now().Unix() / 30
	for offset := -int64(drift); offset <= int64(drift); offset++ {
		got, err := totpAt(secret, now+offset)
		if err != nil {
			return false, err
		}
		if hmac.Equal([]byte(got), []byte(code)) {
			return true, nil
		}
	}
	return false, nil
}

// TOTPProvisioningURI returns the otpauth:// URI suitable for encoding
// in a QR code so authenticator apps can scan and add the account.
//
//	otpauth://totp/MX%20Script:user@host?secret=BASE32&issuer=MX%20Script
func TOTPProvisioningURI(account, secret, issuer string) string {
	if issuer == "" {
		issuer = "MX Script"
	}
	// The label is "<issuer>:<account>". Per Google's spec each side
	// is percent-encoded individually so reserved characters in the
	// account (like `@`) are escaped, while the literal `:` separator
	// stays unescaped.
	label := url.QueryEscape(issuer) + ":" + url.QueryEscape(account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// totpAt computes the 6-digit TOTP code for a specific counter. Uses
// HMAC-SHA1 with dynamic truncation per RFC 4226.
func totpAt(secret string, counter int64) (string, error) {
	// Authenticator apps emit base32 in upper-case without padding;
	// users sometimes paste it lower-case, with spaces, or with
	// trailing `=` padding. Normalise: upper, no whitespace, strip
	// any existing padding, then add back exactly enough to reach a
	// multiple of 8 characters.
	clean := strings.ToUpper(strings.TrimSpace(secret))
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.TrimRight(clean, "=")
	for len(clean)%8 != 0 {
		clean += "="
	}
	keyBytes, err := base32.StdEncoding.DecodeString(clean)
	if err != nil {
		return "", fmt.Errorf("totp secret must be base32: %w", err)
	}
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], uint64(counter))
	mac := hmac.New(sha1.New, keyBytes)
	mac.Write(counterBytes[:])
	hash := mac.Sum(nil)
	// Dynamic truncation — RFC 4226 §5.3.
	offset := hash[len(hash)-1] & 0x0F
	binCode := uint32(hash[offset]&0x7F)<<24 |
		uint32(hash[offset+1])<<16 |
		uint32(hash[offset+2])<<8 |
		uint32(hash[offset+3])
	otp := binCode % 1_000_000
	return fmt.Sprintf("%06d", otp), nil
}

// hmacSHA256Hex shadows the existing private helper for use inside
// magic-link signing. Defined locally so the two flows stay self-
// contained even if the rest of crypto.go is restructured.
func hmacSHA256Hex(secret, message string) string {
	return computeHMACHex(secret, message)
}

// ===== Builtin shims =====

func builtinTOTPGenerate(i *Interpreter, args []Value) (Value, error) {
	secret, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	code, err := TOTPGenerate(secret)
	if err != nil {
		return Value{}, err
	}
	return StringValue(code), nil
}

func builtinTOTPVerify(i *Interpreter, args []Value) (Value, error) {
	code, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	secret, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	drift := 1
	if len(args) > 2 && args[2].Kind == KindNumber {
		drift = int(args[2].Number)
	}
	ok, err := TOTPVerify(code, secret, drift)
	if err != nil {
		return Value{}, err
	}
	return BoolValue(ok), nil
}

func builtinTOTPURI(i *Interpreter, args []Value) (Value, error) {
	account, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	secret, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	issuer := ""
	if len(args) > 2 && args[2].Kind == KindString {
		issuer = args[2].String
	}
	return StringValue(TOTPProvisioningURI(account, secret, issuer)), nil
}
