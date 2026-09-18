package secure

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestVaultRoundTrip(t *testing.T) {
	vault, err := OpenVault(filepath.Join(t.TempDir(), "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"accessToken":"secret"}`)
	sealed, err := vault.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("secret")) {
		t.Fatal("ciphertext leaked plaintext")
	}
	opened, err := vault.Decrypt(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, plain) {
		t.Fatalf("round trip mismatch: %s", opened)
	}
}
