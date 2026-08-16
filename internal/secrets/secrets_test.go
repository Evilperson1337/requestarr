package secrets

import (
	"bytes"
	"errors"
	"testing"
)

func testContext() Context {
	return Context{Namespace: "oidc", OwnerID: OwnerID{1}, Name: "client_secret"}
}

func TestEncryptDecryptAndNonceUniqueness(t *testing.T) {
	key := [32]byte{7}
	random := bytes.NewReader(append(bytes.Repeat([]byte{1}, NonceSize), bytes.Repeat([]byte{2}, NonceSize)...))
	c := NewWithRandom(key, random)
	first, err := c.Encrypt(testContext(), []byte("distinctive plaintext"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Encrypt(testContext(), []byte("distinctive plaintext"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Nonce == second.Nonce || bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatal("fresh randomness did not produce a unique envelope")
	}
	got, err := c.Decrypt(testContext(), first)
	if err != nil || string(got) != "distinctive plaintext" {
		t.Fatalf("Decrypt() = %q, %v", got, err)
	}
	if bytes.Contains(first.Ciphertext, []byte("distinctive plaintext")) {
		t.Fatal("ciphertext contains plaintext")
	}
}

func TestDecryptRejectsTamperContextAndWrongKey(t *testing.T) {
	c := NewWithRandom([32]byte{1}, bytes.NewReader(bytes.Repeat([]byte{3}, NonceSize)))
	envelope, err := c.Encrypt(testContext(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := envelope
	tampered.Ciphertext = append([]byte(nil), envelope.Ciphertext...)
	tampered.Ciphertext[0] ^= 1
	if _, err := c.Decrypt(testContext(), tampered); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("tamper error = %v", err)
	}
	other := testContext()
	other.Name = "other"
	if _, err := c.Decrypt(other, envelope); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("context error = %v", err)
	}
	other = testContext()
	other.OwnerID[1] = 1
	if _, err := c.Decrypt(other, envelope); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("owner error = %v", err)
	}
	if _, err := New([32]byte{2}).Decrypt(testContext(), envelope); !errors.Is(err, ErrKeyIncompatible) {
		t.Fatalf("key error = %v", err)
	}
}

func TestCipherBoundsAndRandomFailure(t *testing.T) {
	c := NewWithRandom([32]byte{1}, bytes.NewReader(nil))
	if _, err := c.Encrypt(testContext(), make([]byte, MaxPlaintext+1)); !errors.Is(err, ErrPlaintextTooLarge) {
		t.Fatalf("large error = %v", err)
	}
	if _, err := c.Encrypt(testContext(), nil); !errors.Is(err, ErrRandomUnavailable) {
		t.Fatalf("random error = %v", err)
	}
	if _, err := New([32]byte{1}).Decrypt(testContext(), Envelope{Version: 2}); !errors.Is(err, ErrEnvelopeInvalid) {
		t.Fatalf("version error = %v", err)
	}
	if _, err := New([32]byte{1}).Encrypt(Context{}, nil); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("context error = %v", err)
	}
}
