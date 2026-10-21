package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestPKCEChallengeUsesS256(t *testing.T) {
	got := PKCEChallenge("verifier-value")
	h := sha256.Sum256([]byte("verifier-value"))
	want := base64.RawURLEncoding.EncodeToString(h[:])
	if got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
}

func TestRandomValueIsUniqueAndURLSafe(t *testing.T) {
	a, err := RandomValue()
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomValue()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("random values collided")
	}
	if _, err := base64.RawURLEncoding.DecodeString(a); err != nil {
		t.Fatalf("value is not URL safe base64: %v", err)
	}
}
