package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// LoginCookieCodec seals the temporary login transaction cookie with AES-GCM.
// The key is deployment-provided and never written to the browser.
type LoginCookieCodec struct{ aead cipher.AEAD }

func NewLoginCookieCodec(key [32]byte) (*LoginCookieCodec, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &LoginCookieCodec{aead: aead}, nil
}

func (c *LoginCookieCodec) Seal(value LoginCookie) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("login cookie codec unavailable")
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(append(nonce, c.aead.Seal(nil, nonce, plain, []byte("requestarr/auth/login/v1"))...)), nil
}

func (c *LoginCookieCodec) Open(value string) (LoginCookie, error) {
	if c == nil || c.aead == nil {
		return LoginCookie{}, errors.New("login cookie codec unavailable")
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) < c.aead.NonceSize()+c.aead.Overhead() {
		return LoginCookie{}, errors.New("login cookie invalid")
	}
	plain, err := c.aead.Open(nil, raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():], []byte("requestarr/auth/login/v1"))
	if err != nil {
		return LoginCookie{}, errors.New("login cookie invalid")
	}
	var decoded LoginCookie
	if json.Unmarshal(plain, &decoded) != nil || decoded.ExpiresAt.Before(time.Now()) {
		return LoginCookie{}, errors.New("login cookie invalid")
	}
	if _, err := ParseState(string(decoded.State)); err != nil {
		return LoginCookie{}, err
	}
	if _, err := ParseNonce(string(decoded.Nonce)); err != nil {
		return LoginCookie{}, err
	}
	if _, err := ParsePKCEVerifier(string(decoded.PKCEVerifier)); err != nil {
		return LoginCookie{}, err
	}
	return decoded, nil
}
