// Package secretbox seals small secrets (repo deploy tokens, a runner's state
// file) at rest with AES-256-GCM. Extracted from bbux
// apps/api/internal/deploy/crypto.go; the wire format is unchanged, so values
// bbux already stored still decrypt.
//
// The AES key is derived from a configured secret string via SHA-256, so the
// secret can be any length. A fresh random 12-byte GCM nonce is generated per
// seal and prepended to the output:
//
//	secret string ──SHA-256──▶ 32-byte Key
//
//	Encrypt:  plaintext ──AES-256-GCM(Key, nonce)──▶ [ nonce (12) | ciphertext + tag (16) ]
//	Decrypt:  [ nonce | ciphertext + tag ] ──split──▶ GCM.Open ──▶ plaintext
//	                                                   (wrong key / tampered → error)
//
// An empty plaintext seals to nil ("nothing to store"), and nil/empty input
// opens to "" — so an optional secret column can stay NULL.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Key is a 32-byte AES-256 key. Build one with DeriveKey.
type Key [32]byte

// ErrTooShort is returned when sealed input is shorter than a GCM nonce.
var ErrTooShort = errors.New("secretbox: ciphertext too short")

// DeriveKey turns a secret of any length into an AES-256 key (SHA-256).
func DeriveKey(secret string) Key {
	return Key(sha256.Sum256([]byte(secret)))
}

// Encrypt seals plaintext. An empty plaintext returns (nil, nil).
func Encrypt(key Key, plaintext string) ([]byte, error) {
	return EncryptBytes(key, []byte(plaintext))
}

// Decrypt opens what Encrypt sealed. Nil/empty input returns ("", nil).
func Decrypt(key Key, sealed []byte) (string, error) {
	out, err := DecryptBytes(key, sealed)
	return string(out), err
}

// EncryptBytes is Encrypt for arbitrary bytes. Empty input returns (nil, nil).
func EncryptBytes(key Key, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secretbox: nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptBytes is Decrypt for arbitrary bytes. Empty input returns (nil, nil).
func DecryptBytes(key Key, sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(sealed) < ns {
		return nil, ErrTooShort
	}
	out, err := gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("secretbox: decrypt: %w", err)
	}
	return out, nil
}

// Scrub redacts secret from text (e.g. build logs) so it can't leak through
// echoed output. No-op for an empty secret.
func Scrub(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "***redacted***")
}

func newGCM(key Key) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("secretbox: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: new gcm: %w", err)
	}
	return gcm, nil
}
