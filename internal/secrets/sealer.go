// Package secrets holds Requestarr's stored integration secrets (D024): the
// registry of secret names, the master-key sealer, and the pure rules that pick
// which source a secret's effective value comes from. It imports no HTTP or
// database code; persistence sits behind Repository.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// MasterKeyEnv is the environment variable that carries the master key.
const MasterKeyEnv = "REQUESTARR_SECRETS_KEY"

const keyLen = 32

var (
	// ErrMasterKey reports a malformed master key. It never contains the key.
	ErrMasterKey = errors.New("REQUESTARR_SECRETS_KEY must be 32 bytes encoded as 64 hex characters or base64")
	// ErrUnreadable means a stored secret could not be opened with the current key.
	ErrUnreadable = errors.New("stored secret unreadable")
)

// Sealed is an encrypted secret with the id of the key that sealed it.
type Sealed struct {
	Ciphertext []byte
	KeyID      string
}

// Sealer encrypts and decrypts secret values. Open must fail closed: a wrong
// key, tampered ciphertext, mismatched key id, or a ciphertext moved to
// another secret name never yields a value.
type Sealer interface {
	Seal(name Name, plaintext []byte) (Sealed, error)
	Open(name Name, sealed Sealed) ([]byte, error)
}

// ParseMasterKey decodes a 32-byte key given as 64 hex characters or base64
// (standard or URL-safe, padded or not). The error never echoes the input.
func ParseMasterKey(raw string) ([]byte, error) {
	if len(raw) == hex.EncodedLen(keyLen) {
		if key, err := hex.DecodeString(raw); err == nil {
			return key, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(raw); err == nil && len(key) == keyLen {
			return key, nil
		}
	}
	return nil, ErrMasterKey
}

type aesGCM struct {
	aead  cipher.AEAD
	keyID string
}

// NewAESGCM returns an AES-256-GCM Sealer. The ciphertext is bound to the secret
// name as additional data. The key id is a one-way digest prefix of the key.
func NewAESGCM(key []byte) (Sealer, error) {
	if len(key) != keyLen {
		return nil, ErrMasterKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrMasterKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrMasterKey
	}
	digest := sha256.Sum256(append([]byte("requestarr/secrets/key-id/v1:"), key...))
	return &aesGCM{aead: aead, keyID: hex.EncodeToString(digest[:6])}, nil
}

func (s *aesGCM) Seal(name Name, plaintext []byte) (Sealed, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Sealed{}, fmt.Errorf("generate nonce: %w", err)
	}
	return Sealed{Ciphertext: s.aead.Seal(nonce, nonce, plaintext, []byte(name)), KeyID: s.keyID}, nil
}

func (s *aesGCM) Open(name Name, sealed Sealed) ([]byte, error) {
	n := s.aead.NonceSize()
	if sealed.KeyID != s.keyID || len(sealed.Ciphertext) < n+s.aead.Overhead() {
		return nil, ErrUnreadable
	}
	plain, err := s.aead.Open(nil, sealed.Ciphertext[:n], sealed.Ciphertext[n:], []byte(name))
	if err != nil {
		return nil, ErrUnreadable
	}
	return plain, nil
}
