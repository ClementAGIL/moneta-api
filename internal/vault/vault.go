// Package vault does the envelope encryption: a master key (KEK) wraps one random data key (DEK) per user; from the
// DEK come an AES-GCM key for sensitive text and an HMAC key for blind indexes (equality lookups without decrypting).
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const keySize = 32

var errCiphertext = errors.New("ciphertext too short")

// KEK is the master key from the environment. Losing it makes every user's data unreadable.
type KEK struct{ aead cipher.AEAD }

// ParseKEK decodes a base64 32-byte key.
func ParseKEK(b64 string) (*KEK, error) {
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(key) != keySize {
		return nil, fmt.Errorf("master key must be %d random bytes in base64", keySize)
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &KEK{aead: aead}, nil
}

// NewWrappedDEK creates a user's data key and returns it wrapped, bound to the user id.
func (k *KEK) NewWrappedDEK(userID uuid.UUID) []byte {
	return seal(k.aead, randomBytes(keySize), userID[:])
}

// Unwrap returns the keys of a user from the stored wrapped DEK.
func (k *KEK) Unwrap(userID uuid.UUID, wrapped []byte) (*UserKeys, error) {
	dek, err := open(k.aead, wrapped, userID[:])
	if err != nil {
		return nil, fmt.Errorf("unwrapping user key: %w", err)
	}
	encKey, err := hkdf.Key(sha256.New, dek, nil, "moneta/encrypt", keySize)
	if err != nil {
		return nil, err
	}
	indexKey, err := hkdf.Key(sha256.New, dek, nil, "moneta/index", keySize)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(encKey)
	if err != nil {
		return nil, err
	}
	return &UserKeys{aead: aead, indexKey: indexKey}, nil
}

// UserKeys encrypts one user's sensitive text.
type UserKeys struct {
	aead     cipher.AEAD
	indexKey []byte
}

// Seal encrypts text (nonce prepended).
func (u *UserKeys) Seal(text string) []byte {
	return seal(u.aead, []byte(text), nil)
}

// Open decrypts what Seal produced.
func (u *UserKeys) Open(data []byte) (string, error) {
	plain, err := open(u.aead, data, nil)
	return string(plain), err
}

// Index returns a deterministic HMAC of value, for equality lookups on encrypted columns.
func (u *UserKeys) Index(value string) []byte {
	mac := hmac.New(sha256.New, u.indexKey)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(aead cipher.AEAD, plain, ad []byte) []byte {
	nonce := randomBytes(aead.NonceSize())
	return aead.Seal(nonce, nonce, plain, ad)
}

func open(aead cipher.AEAD, data, ad []byte) ([]byte, error) {
	n := aead.NonceSize()
	if len(data) < n {
		return nil, errCiphertext
	}
	return aead.Open(nil, data[:n], data[n:], ad)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never fails since Go 1.24 (it crashes the program instead)
	return b
}
