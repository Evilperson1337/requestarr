// Package password hashes and verifies the local administrator's password
// (D040) with PBKDF2-HMAC-SHA256 from the standard library.
//
// A stored hash reads "pbkdf2-sha256$<iterations>$<salt>$<hash>" (unpadded
// standard base64), so the scheme and cost travel with the hash and can be
// raised later: Verify honours whatever the hash says, NeedsRehash reports a
// hash that is weaker than the current settings.
package password

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	scheme = "pbkdf2-sha256"
	// saltBytes is the per-password random salt length.
	saltBytes = 16
	// keyBytes is the derived key length (one SHA-256 block).
	keyBytes = sha256.Size
	// maxIterations bounds the cost a stored hash may ask for, so a corrupt
	// or hostile value cannot make Verify run for minutes.
	maxIterations = 10_000_000
	// alphabet leaves out 0 O 1 l I and punctuation, so a password read from
	// a log line is typed without guessing.
	alphabet = "ABCDEFGHJKMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789"
	// randomLength gives about 138 bits of entropy.
	randomLength = 24
)

// Length limits for a chosen password, in characters. There are no
// composition rules.
const (
	MinLength = 12
	MaxLength = 128
)

// ValidLength reports whether pw is a valid chosen password.
func ValidLength(pw string) bool {
	n := utf8.RuneCountInString(pw)
	return utf8.ValidString(pw) && n >= MinLength && n <= MaxLength
}

// Default is the current cost: OWASP's 2023 guidance for PBKDF2-HMAC-SHA256.
var Default = Hasher{Iterations: 600_000}

// Hasher hashes at a fixed cost. The zero value uses Default.
type Hasher struct{ Iterations int }

func (h Hasher) iterations() int {
	if h.Iterations <= 0 {
		return Default.Iterations
	}
	return h.Iterations
}

// Hash returns the encoded hash of pw with a fresh random salt.
func (h Hasher) Hash(pw string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	n := h.iterations()
	key, err := pbkdf2.Key(sha256.New, pw, salt, n, keyBytes)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{scheme, strconv.Itoa(n), b64(salt), b64(key)}, "$"), nil
}

// Verify reports whether pw matches the encoded hash, comparing in constant
// time. A malformed encoding never verifies.
func (Hasher) Verify(pw, encoded string) bool {
	n, salt, want, ok := parse(encoded)
	if !ok {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, n, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NeedsRehash reports a hash that is malformed or weaker than h's settings.
func (h Hasher) NeedsRehash(encoded string) bool {
	n, _, _, ok := parse(encoded)
	return !ok || n < h.iterations()
}

// Random returns a new password of 24 characters drawn from an unambiguous
// alphabet with crypto/rand.
func Random() (string, error) {
	out := make([]byte, randomLength)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

func parse(encoded string) (iterations int, salt, key []byte, ok bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != scheme {
		return 0, nil, nil, false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 1 || n > maxIterations {
		return 0, nil, nil, false
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < saltBytes {
		return 0, nil, nil, false
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(key) != keyBytes {
		return 0, nil, nil, false
	}
	return n, salt, key, true
}

func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }
