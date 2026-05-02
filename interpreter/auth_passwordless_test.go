package interpreter

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMagicLinkRoundtrip(t *testing.T) {
	secret := "test-secret-key"
	email := "user@example.com"
	token := MagicLinkCreate(email, secret, 15)
	got, err := MagicLinkVerify(token, secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != email {
		t.Errorf("email: got %q, want %q", got, email)
	}
}

func TestMagicLinkRejectsTamperedEmail(t *testing.T) {
	secret := "test-secret-key"
	token := MagicLinkCreate("real@example.com", secret, 15)
	// Replace the email portion with a different base64 value but
	// keep the same signature — verification must reject.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("malformed token")
	}
	// "ZXZpbEBleGFtcGxlLmNvbQ" = base64url of "evil@example.com"
	tampered := "ZXZpbEBleGFtcGxlLmNvbQ" + "." + parts[1] + "." + parts[2]
	if _, err := MagicLinkVerify(tampered, secret); err == nil {
		t.Error("tampered email should not verify")
	}
}

func TestMagicLinkRejectsExpiredToken(t *testing.T) {
	secret := "test-secret-key"
	// Use minutes <= 0 to force the default 15, then mutate the
	// expiry field down to something already past.
	token := MagicLinkCreate("user@example.com", secret, 15)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("malformed token")
	}
	// Set expiry to 5 minutes ago and re-sign.
	pastTs := time.Now().Add(-5 * time.Minute).Unix()
	signedString := parts[0] + "." + strconv.FormatInt(pastTs, 10)
	expiredToken := signedString + "." + computeHMACHex(secret, signedString)
	if _, err := MagicLinkVerify(expiredToken, secret); err == nil {
		t.Error("expired token should not verify")
	}
}

func TestMagicLinkRejectsWrongSecret(t *testing.T) {
	token := MagicLinkCreate("user@example.com", "key-1", 15)
	if _, err := MagicLinkVerify(token, "key-2"); err == nil {
		t.Error("wrong secret should not verify")
	}
}

func TestMagicLinkBuiltinReturnsNullOnFailure(t *testing.T) {
	// The route-handler convention is `email == null` on failure, not
	// thrown errors. Confirm the builtin honors that.
	v, err := builtinMagicLinkVerify(nil, []Value{
		StringValue("not.a.token"),
		StringValue("any-secret"),
	})
	if err != nil {
		t.Fatalf("builtin should not throw: %v", err)
	}
	if v.Kind != KindNull {
		t.Errorf("got %+v, want null", v)
	}
}

func TestTOTPRoundtripCurrentCode(t *testing.T) {
	// "JBSWY3DPEHPK3PXP" is "Hello!" base32, a common test vector.
	secret := "JBSWY3DPEHPK3PXP"
	code, err := TOTPGenerate(secret)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("code length: got %d, want 6", len(code))
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("code contains non-digit: %s", code)
			break
		}
	}
	// Verifying the just-generated code must succeed.
	ok, err := TOTPVerify(code, secret, 1)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Errorf("verify should accept just-generated code %s", code)
	}
}

func TestTOTPRejectsWrongCode(t *testing.T) {
	ok, err := TOTPVerify("000000", "JBSWY3DPEHPK3PXP", 1)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	// 000000 is incredibly unlikely to be the current code; if this
	// flakes, the universe owes us a beer.
	if ok {
		t.Skip("000000 happened to be the current code — re-run")
	}
}

func TestTOTPSecretIsCaseAndPaddingTolerant(t *testing.T) {
	// Authenticator apps emit upper-case base32 with no padding, but
	// users sometimes paste with lower-case or spaces or padding.
	cases := []string{
		"JBSWY3DPEHPK3PXP",
		"jbswy3dpehpk3pxp",
		"JBSW Y3DP EHPK 3PXP",
		"JBSWY3DPEHPK3PXP====",
	}
	codes := make([]string, len(cases))
	for k, s := range cases {
		c, err := TOTPGenerate(s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		codes[k] = c
	}
	// All four input forms must produce the same 6-digit output.
	for k := 1; k < len(codes); k++ {
		if codes[k] != codes[0] {
			t.Errorf("variant %d: got %s, want %s", k, codes[k], codes[0])
		}
	}
}

func TestTOTPProvisioningURI(t *testing.T) {
	uri := TOTPProvisioningURI("alice@example.com", "JBSWY3DPEHPK3PXP", "Acme")
	wantParts := []string{
		"otpauth://totp/",
		"Acme",
		"alice%40example.com",
		"secret=JBSWY3DPEHPK3PXP",
		"issuer=Acme",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
	}
	for _, want := range wantParts {
		if !strings.Contains(uri, want) {
			t.Errorf("uri %q missing %q", uri, want)
		}
	}
}

func TestTOTPDriftWindow(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	// Generate a code for 30 seconds ago manually, then verify with
	// drift=1 (which allows ±30s) to confirm the slot search works.
	pastCounter := time.Now().Unix()/30 - 1
	pastCode, err := totpAt(secret, pastCounter)
	if err != nil {
		t.Fatalf("totpAt: %v", err)
	}
	ok, err := TOTPVerify(pastCode, secret, 1)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Error("drift=1 should accept code from 30s ago")
	}
	// drift=0 must reject the same code.
	ok, _ = TOTPVerify(pastCode, secret, 0)
	if ok {
		t.Error("drift=0 should reject code from 30s ago")
	}
}
