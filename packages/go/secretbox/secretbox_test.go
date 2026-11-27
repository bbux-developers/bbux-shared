package secretbox

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"testing"
)

// Ported from bbux apps/api/internal/deploy/deploy_test.go.

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := DeriveKey("a-secret")
	secret := "ghp_supersecrettoken1234567890"
	sealed, err := Encrypt(key, secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(sealed) == 0 {
		t.Fatal("expected non-empty ciphertext")
	}
	if bytes.Contains(sealed, []byte(secret)) {
		t.Fatal("ciphertext leaks the plaintext")
	}
	out, err := Decrypt(key, sealed)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if out != secret {
		t.Fatalf("round-trip = %q, want %q", out, secret)
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	key := DeriveKey("k")
	a, _ := Encrypt(key, "same")
	b, _ := Encrypt(key, "same")
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext must differ (fresh nonce)")
	}
}

func TestEncryptEmptyReturnsNil(t *testing.T) {
	key := DeriveKey("x")
	sealed, err := Encrypt(key, "")
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}
	if sealed != nil {
		t.Fatalf("expected nil for empty plaintext, got %v", sealed)
	}
	out, err := Decrypt(key, nil)
	if err != nil || out != "" {
		t.Fatalf("decrypt nil = (%q, %v), want (\"\", nil)", out, err)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	sealed, err := Encrypt(DeriveKey("right"), "payload")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := Decrypt(DeriveKey("wrong"), sealed); err == nil {
		t.Fatal("expected decryption with the wrong key to fail")
	}
}

func TestDecryptTamperedFails(t *testing.T) {
	key := DeriveKey("k")
	sealed, _ := Encrypt(key, "payload")
	sealed[len(sealed)-1] ^= 0xff
	if _, err := Decrypt(key, sealed); err == nil {
		t.Fatal("expected a tampered ciphertext to fail")
	}
}

func TestDecryptTooShort(t *testing.T) {
	if _, err := Decrypt(DeriveKey("k"), []byte{1, 2, 3}); !errors.Is(err, ErrTooShort) {
		t.Fatalf("err = %v, want ErrTooShort", err)
	}
}

// TestWireCompatibleWithBbux seals with the exact construction bbux's
// deploy/crypto.go used (sha256 key, 12-byte nonce prefix, no AAD) and checks
// secretbox opens it — so rows bbux already stored keep decrypting after G3.
func TestWireCompatibleWithBbux(t *testing.T) {
	raw := sha256.Sum256([]byte("dev-insecure-deploy-key-secret"))
	block, _ := aes.NewCipher(raw[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := bytes.Repeat([]byte{7}, gcm.NonceSize())
	sealed := gcm.Seal(nonce, nonce, []byte("ghp_legacy"), nil)

	got, err := Decrypt(DeriveKey("dev-insecure-deploy-key-secret"), sealed)
	if err != nil || got != "ghp_legacy" {
		t.Fatalf("Decrypt legacy = (%q, %v), want (ghp_legacy, nil)", got, err)
	}
}

func TestBytesRoundTrip(t *testing.T) {
	key := DeriveKey("k")
	in := []byte{0, 1, 2, 255}
	sealed, err := EncryptBytes(key, in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecryptBytes(key, sealed)
	if err != nil || !bytes.Equal(out, in) {
		t.Fatalf("bytes round-trip = (%v, %v)", out, err)
	}
}

func TestScrub(t *testing.T) {
	log := "cloning with token ghp_abc123 into /tmp/build"
	got := Scrub(log, "ghp_abc123")
	if bytes.Contains([]byte(got), []byte("ghp_abc123")) {
		t.Fatalf("scrub left the secret in: %q", got)
	}
	if Scrub(log, "") != log {
		t.Fatal("empty secret must be a no-op")
	}
}
