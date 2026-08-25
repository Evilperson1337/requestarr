package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestLoginCookieCodecRoundTripAndRejectsTampering(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	codec, err := NewLoginCookieCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	value := LoginCookie{State: State(opaqueTestValue(1)), Nonce: Nonce(opaqueTestValue(2)), PKCEVerifier: PKCEVerifier(strings.Repeat("c", 64)), ExpiresAt: time.Now().Add(time.Minute)}
	sealed, err := codec.Seal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Open(sealed)
	if err != nil || decoded.State != value.State || decoded.Nonce != value.Nonce || decoded.PKCEVerifier != value.PKCEVerifier {
		t.Fatalf("Open() = %#v, %v", decoded, err)
	}
	if _, err := codec.Open(sealed[:len(sealed)-1] + "A"); err == nil {
		t.Fatal("Open() accepted a modified cookie")
	}
}

func opaqueTestValue(byteValue byte) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat(string(byteValue), 32)))
}
