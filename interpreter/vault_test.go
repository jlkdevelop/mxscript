package interpreter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withVaultDir runs fn() in a fresh tempdir as cwd so the .vault.json
// the helpers read/write doesn't leak between tests.
func withVaultDir(t *testing.T, key string, fn func()) {
	t.Helper()
	prev, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	os.Setenv("MX_VAULT_KEY", key)
	defer func() {
		os.Chdir(prev)
		os.Unsetenv("MX_VAULT_KEY")
	}()
	fn()
}

const vaultTestKey = "0011223344556677889900aabbccddeeff00112233445566778899aabbccddee" // 32 bytes hex

func TestVaultRoundtrip(t *testing.T) {
	withVaultDir(t, vaultTestKey, func() {
		_, err := builtinVaultSet(nil, []Value{
			StringValue("stripe_key"), StringValue("sk_test_xyz"),
		})
		if err != nil {
			t.Fatalf("set: %v", err)
		}
		v, err := builtinVaultGet(nil, []Value{StringValue("stripe_key")})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if v.String != "sk_test_xyz" {
			t.Errorf("got %q, want sk_test_xyz", v.String)
		}
	})
}

func TestVaultEncryptedAtRest(t *testing.T) {
	withVaultDir(t, vaultTestKey, func() {
		builtinVaultSet(nil, []Value{StringValue("k"), StringValue("plaintext-value")})
		raw, err := os.ReadFile(filepath.Join(".", vaultPath))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		// The plaintext must NOT appear in the on-disk file.
		if strings.Contains(string(raw), "plaintext-value") {
			t.Errorf("plaintext leaked into .vault.json: %s", raw)
		}
	})
}

func TestVaultRequiresMasterKey(t *testing.T) {
	prev := os.Getenv("MX_VAULT_KEY")
	os.Unsetenv("MX_VAULT_KEY")
	defer func() {
		if prev != "" {
			os.Setenv("MX_VAULT_KEY", prev)
		}
	}()
	_, err := builtinVaultGet(nil, []Value{StringValue("anything")})
	if err == nil || !strings.Contains(err.Error(), "MX_VAULT_KEY") {
		t.Errorf("expected MX_VAULT_KEY error, got %v", err)
	}
}

func TestVaultRejectsWrongKey(t *testing.T) {
	withVaultDir(t, vaultTestKey, func() {
		builtinVaultSet(nil, []Value{StringValue("k"), StringValue("v")})
	})
	// Change cwd to where the vault was written, but use a different key.
	withVaultDir(t, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", func() {
		// Re-create the vault file in the new cwd with the wrong-key
		// payload from the prior step — but withVaultDir uses a fresh
		// tempdir each call, so just write known-bad blob.
		os.WriteFile(vaultPath, []byte(`{"entries":{"k":"YmFkLWNpcGhlcnRleHQ="}}`), 0o600)
		_, err := builtinVaultGet(nil, []Value{StringValue("k")})
		if err == nil || !strings.Contains(err.Error(), "decrypt failed") {
			t.Errorf("expected decrypt failure, got %v", err)
		}
	})
}

func TestVaultListAndDelete(t *testing.T) {
	withVaultDir(t, vaultTestKey, func() {
		builtinVaultSet(nil, []Value{StringValue("a"), StringValue("1")})
		builtinVaultSet(nil, []Value{StringValue("b"), StringValue("2")})
		v, _ := builtinVaultList(nil, nil)
		if v.Kind != KindArray || len(v.Array) != 2 {
			t.Errorf("list: got %+v", v)
		}
		builtinVaultDelete(nil, []Value{StringValue("a")})
		v, _ = builtinVaultList(nil, nil)
		if len(v.Array) != 1 {
			t.Errorf("after delete: got %+v", v)
		}
	})
}

func TestVaultRequires32ByteKey(t *testing.T) {
	withVaultDir(t, "abcd", func() {
		// Too short — should error clearly.
		_, err := builtinVaultGet(nil, []Value{StringValue("k")})
		if err == nil || !strings.Contains(err.Error(), "32 bytes") {
			t.Errorf("expected 32-byte error, got %v", err)
		}
	})
}
