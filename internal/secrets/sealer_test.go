package secrets_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

func key32(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func TestParseMasterKeyAcceptsHexAndBase64Of32Bytes(t *testing.T) {
	want := key32(7)
	for name, raw := range map[string]string{
		"hex":       hex.EncodeToString(want),
		"base64":    base64.StdEncoding.EncodeToString(want),
		"base64url": base64.RawURLEncoding.EncodeToString(want),
	} {
		got, err := secrets.ParseMasterKey(raw)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: got %x err %v", name, got, err)
		}
	}
}

func TestParseMasterKeyRejectsMalformedWithoutEchoingIt(t *testing.T) {
	for _, raw := range []string{"short", strings.Repeat("a", 63), base64.StdEncoding.EncodeToString(key32(1)[:16]), "not base64 or hex at all, but long enough to be 32+ bytes!!"} {
		_, err := secrets.ParseMasterKey(raw)
		if err == nil {
			t.Errorf("%q accepted", raw)
		} else if strings.Contains(err.Error(), raw) {
			t.Errorf("error echoes the key: %v", err)
		}
	}
}

func TestSealerRoundTrip(t *testing.T) {
	s, err := secrets.NewAESGCM(key32(1))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal(secrets.TMDBReadToken, []byte("tmdb-value"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed.Ciphertext, []byte("tmdb-value")) {
		t.Fatal("ciphertext contains plaintext")
	}
	if sealed.KeyID == "" || strings.Contains(sealed.KeyID, hex.EncodeToString(key32(1))) {
		t.Fatalf("bad key id %q", sealed.KeyID)
	}
	got, err := s.Open(secrets.TMDBReadToken, sealed)
	if err != nil || string(got) != "tmdb-value" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestSealerUsesFreshNonces(t *testing.T) {
	s, _ := secrets.NewAESGCM(key32(1))
	a, _ := s.Seal(secrets.TMDBReadToken, []byte("v"))
	b, _ := s.Seal(secrets.TMDBReadToken, []byte("v"))
	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Fatal("ciphertext repeated")
	}
}

func TestSealerFailsClosed(t *testing.T) {
	s, _ := secrets.NewAESGCM(key32(1))
	other, _ := secrets.NewAESGCM(key32(2))
	sealed, _ := s.Seal(secrets.TMDBReadToken, []byte("tmdb-value"))

	if _, err := other.Open(secrets.TMDBReadToken, sealed); err == nil {
		t.Error("wrong key opened the secret")
	}
	tampered := secrets.Sealed{Ciphertext: append([]byte(nil), sealed.Ciphertext...), KeyID: sealed.KeyID}
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 1
	if _, err := s.Open(secrets.TMDBReadToken, tampered); err == nil {
		t.Error("tampered ciphertext opened")
	}
	if _, err := s.Open(secrets.TMDBReadToken, secrets.Sealed{Ciphertext: sealed.Ciphertext, KeyID: "other"}); err == nil {
		t.Error("key id mismatch opened")
	}
	if _, err := s.Open(secrets.Name("igdb_client_secret"), sealed); err == nil {
		t.Error("ciphertext moved to another secret name opened")
	}
	if _, err := s.Open(secrets.TMDBReadToken, secrets.Sealed{Ciphertext: []byte{1, 2}, KeyID: sealed.KeyID}); err == nil {
		t.Error("truncated ciphertext opened")
	}
}
