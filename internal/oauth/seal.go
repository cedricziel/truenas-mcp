package oauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidToken means a sealed value failed to decrypt or verify: it was
// tampered with, sealed under a different master key (including one
// generated for a since-restarted process), sealed for a different purpose,
// or is simply not a value this package produced.
var ErrInvalidToken = errors.New("invalid or expired token")

// seal encrypts payload (JSON-marshaled) with AES-256-GCM under keys.aead,
// binding it to purpose as additional authenticated data. Binding purpose
// this way means a value sealed as, say, a refresh token cannot be replayed
// as an access token even though both are sealed under the same key: open
// with a different purpose fails.
func seal(keys Keys, purpose string, payload any) (string, error) {
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling token payload: %w", err)
	}

	gcm, err := newGCM(keys)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, []byte(purpose))
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// open reverses seal, verifying purpose and unmarshaling the payload into
// out. Errors never distinguish "tampered" from "wrong purpose" from
// "malformed": all three are ErrInvalidToken, so a caller can't learn
// anything about why a forged value failed.
func open(keys Keys, purpose, token string, out any) error {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return ErrInvalidToken
	}

	gcm, err := newGCM(keys)
	if err != nil {
		return err
	}

	if len(raw) < gcm.NonceSize() {
		return ErrInvalidToken
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(purpose))
	if err != nil {
		return ErrInvalidToken
	}

	if err := json.Unmarshal(plaintext, out); err != nil {
		return ErrInvalidToken
	}
	return nil
}

func newGCM(keys Keys) (cipher.AEAD, error) {
	block, err := aes.NewCipher(keys.aead[:])
	if err != nil {
		return nil, fmt.Errorf("building cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
