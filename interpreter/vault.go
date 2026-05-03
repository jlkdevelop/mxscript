// vault.go — tiny encrypted secrets store. Lets users keep
// per-environment secrets out of the binary + out of plaintext .env
// files. Secrets are AES-256-GCM encrypted at rest with a single
// master key (read from env), so you can commit the encrypted vault
// to source control without leaking the values.
//
//	$ MX_VAULT_KEY=$(openssl rand -hex 32) mx run app.mx
//
//	// app.mx
//	let stripe = vault.get("stripe_key")  // decrypted on read
//
// vault.set / vault.export are the write side: typically you run
// these once interactively to bootstrap the file, then commit the
// resulting .vault.json (encrypted, safe to share).
package interpreter

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const vaultPath = ".vault.json"

// vaultData is the on-disk shape: { entries: { key: <base64-blob> } }.
// Each value is a separate AES-GCM ciphertext so adding a new secret
// doesn't require re-encrypting all the others.
type vaultData struct {
	Entries map[string]string `json:"entries"`
}

var vaultMu sync.Mutex

func vaultMasterKey() ([]byte, error) {
	hexKey := os.Getenv("MX_VAULT_KEY")
	if hexKey == "" {
		return nil, fmt.Errorf("vault.* requires MX_VAULT_KEY (32 random hex bytes)")
	}
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("MX_VAULT_KEY must be hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("MX_VAULT_KEY must decode to 32 bytes (got %d)", len(b))
	}
	return b, nil
}

func vaultEncrypt(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(ciphertext))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func vaultDecrypt(key []byte, blob string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("vault: ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func vaultLoad() (*vaultData, error) {
	raw, err := os.ReadFile(vaultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &vaultData{Entries: map[string]string{}}, nil
		}
		return nil, err
	}
	var v vaultData
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v.Entries == nil {
		v.Entries = map[string]string{}
	}
	return &v, nil
}

func vaultSave(v *vaultData) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(vaultPath, raw, 0o600)
}

// vault.get(key) — decrypt and return the secret. Throws if the key
// doesn't exist or the master key is wrong (GCM auth-tag fails).
func builtinVaultGet(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("vault.get(key) requires a key string")
	}
	mk, err := vaultMasterKey()
	if err != nil {
		return Value{}, err
	}
	vaultMu.Lock()
	defer vaultMu.Unlock()
	data, err := vaultLoad()
	if err != nil {
		return Value{}, err
	}
	blob, ok := data.Entries[args[0].String]
	if !ok {
		return Value{}, fmt.Errorf("vault: no secret named %q", args[0].String)
	}
	plain, err := vaultDecrypt(mk, blob)
	if err != nil {
		return Value{}, fmt.Errorf("vault.get: decrypt failed (wrong key?): %w", err)
	}
	return StringValue(string(plain)), nil
}

// vault.set(key, value) — encrypt and persist. Use interactively
// (e.g. `mx run --eval 'vault.set("stripe_key", "sk_test_...")'`)
// then commit .vault.json. Returns the key on success.
func builtinVaultSet(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindString {
		return Value{}, fmt.Errorf("vault.set(key, value) requires (string, string)")
	}
	mk, err := vaultMasterKey()
	if err != nil {
		return Value{}, err
	}
	blob, err := vaultEncrypt(mk, []byte(args[1].String))
	if err != nil {
		return Value{}, err
	}
	vaultMu.Lock()
	defer vaultMu.Unlock()
	data, err := vaultLoad()
	if err != nil {
		return Value{}, err
	}
	data.Entries[args[0].String] = blob
	if err := vaultSave(data); err != nil {
		return Value{}, err
	}
	return StringValue(args[0].String), nil
}

// vault.list() — returns the keys in the vault. Values are NOT
// decrypted; this is for tooling + sanity checking.
func builtinVaultList(_ *Interpreter, _ []Value) (Value, error) {
	vaultMu.Lock()
	defer vaultMu.Unlock()
	data, err := vaultLoad()
	if err != nil {
		return Value{}, err
	}
	out := make([]Value, 0, len(data.Entries))
	for k := range data.Entries {
		out = append(out, StringValue(k))
	}
	return ArrayValue(out), nil
}

// vault.delete(key) — remove a secret from the vault. No-op when the
// key isn't present.
func builtinVaultDelete(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("vault.delete(key) requires a key string")
	}
	vaultMu.Lock()
	defer vaultMu.Unlock()
	data, err := vaultLoad()
	if err != nil {
		return Value{}, err
	}
	delete(data.Entries, args[0].String)
	return NullValue(), vaultSave(data)
}
