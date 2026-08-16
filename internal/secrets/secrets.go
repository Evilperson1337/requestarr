// Package secrets provides storage-independent authenticated encryption for
// secret values. It deliberately has no HTTP, setup, or database dependency.
package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	EnvelopeVersion = uint8(1)
	NonceSize       = 12
	KeyIDSize       = 16
	MaxPlaintext    = 64 * 1024
	MaxCiphertext   = MaxPlaintext + 16
	MaxNamespace    = 64
	MaxSecretName   = 128
)

var (
	ErrInvalidContext    = errors.New("secret context is invalid")
	ErrPlaintextTooLarge = errors.New("secret plaintext exceeds limit")
	ErrEnvelopeInvalid   = errors.New("secret envelope is invalid")
	ErrKeyIncompatible   = errors.New("secret key is incompatible")
	ErrAuthentication    = errors.New("secret authentication failed")
	ErrRandomUnavailable = errors.New("secret randomness unavailable")
)

type OwnerID [16]byte
type KeyID [KeyIDSize]byte
type Nonce [NonceSize]byte

// Context binds ciphertext to its semantic storage location.
type Context struct {
	Namespace string
	OwnerID   OwnerID
	Name      string
}

// Envelope is the complete, versioned value stored by persistence adapters.
type Envelope struct {
	Version    uint8
	KeyID      KeyID
	Nonce      Nonce
	Ciphertext []byte
}

// Cipher owns one immutable V1 key snapshot and an injectable random source.
type Cipher struct {
	key    [32]byte
	keyID  KeyID
	random io.Reader
}

func New(key [32]byte) *Cipher { return NewWithRandom(key, rand.Reader) }

// NewWithRandom exists for deterministic tests. Production callers use New.
func NewWithRandom(key [32]byte, random io.Reader) *Cipher {
	return &Cipher{key: key, keyID: deriveKeyID(key), random: random}
}

func (c *Cipher) KeyID() KeyID {
	if c == nil {
		return KeyID{}
	}
	return c.keyID
}

func (c *Cipher) Encrypt(ctx Context, plaintext []byte) (Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return Envelope{}, err
	}
	if len(plaintext) > MaxPlaintext {
		return Envelope{}, ErrPlaintextTooLarge
	}
	if c == nil || c.random == nil {
		return Envelope{}, ErrRandomUnavailable
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return Envelope{}, ErrEnvelopeInvalid
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, ErrEnvelopeInvalid
	}
	var nonce Nonce
	if _, err := io.ReadFull(c.random, nonce[:]); err != nil {
		return Envelope{}, ErrRandomUnavailable
	}
	envelope := Envelope{Version: EnvelopeVersion, KeyID: c.keyID, Nonce: nonce}
	envelope.Ciphertext = gcm.Seal(nil, nonce[:], plaintext, associatedData(envelope.Version, ctx))
	return envelope, nil
}

func (c *Cipher) Decrypt(ctx Context, envelope Envelope) ([]byte, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if envelope.Version != EnvelopeVersion || len(envelope.Ciphertext) < 16 || len(envelope.Ciphertext) > MaxCiphertext {
		return nil, ErrEnvelopeInvalid
	}
	if c == nil || !bytes.Equal(envelope.KeyID[:], c.keyID[:]) {
		return nil, ErrKeyIncompatible
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, ErrEnvelopeInvalid
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrEnvelopeInvalid
	}
	plaintext, err := gcm.Open(nil, envelope.Nonce[:], envelope.Ciphertext, associatedData(envelope.Version, ctx))
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}

func validateContext(ctx Context) error {
	if ctx.Namespace == "" || len(ctx.Namespace) > MaxNamespace || !utf8.ValidString(ctx.Namespace) ||
		ctx.Name == "" || len(ctx.Name) > MaxSecretName || !utf8.ValidString(ctx.Name) || ctx.OwnerID == (OwnerID{}) {
		return ErrInvalidContext
	}
	return nil
}

func deriveKeyID(key [32]byte) KeyID {
	h := sha256.New()
	h.Write([]byte("requestarr/encryption/v1/key-id\x00"))
	h.Write(key[:])
	sum := h.Sum(nil)
	var id KeyID
	copy(id[:], sum[:KeyIDSize])
	return id
}

func associatedData(version uint8, ctx Context) []byte {
	result := make([]byte, 0, 1+2+len(ctx.Namespace)+16+2+len(ctx.Name)+32)
	result = append(result, []byte("requestarr/encryption/aad\x00")...)
	result = append(result, version)
	result = binary.BigEndian.AppendUint16(result, uint16(len(ctx.Namespace)))
	result = append(result, ctx.Namespace...)
	result = append(result, ctx.OwnerID[:]...)
	result = binary.BigEndian.AppendUint16(result, uint16(len(ctx.Name)))
	result = append(result, ctx.Name...)
	return result
}
