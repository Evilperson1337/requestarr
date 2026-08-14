package setup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type bootstrapPersistenceFake struct {
	request      ExchangeRequest
	result       ExchangeResult
	err          error
	material     ExchangeMaterial
	materialErr  error
	cleanupCalls int
	cleanupErr   error
}

func (f *bootstrapPersistenceFake) Exchange(_ context.Context, request ExchangeRequest) (ExchangeResult, error) {
	f.request = request
	if request.Material != nil && (f.result.Outcome == ExchangeSessionCreated || f.materialErr != nil) {
		f.material, f.materialErr = request.Material()
		if f.materialErr != nil {
			return ExchangeResult{}, f.materialErr
		}
	}
	return f.result, f.err
}
func (*bootstrapPersistenceFake) ValidateSession(context.Context, Digest) (Session, error) {
	return Session{}, nil
}
func (*bootstrapPersistenceFake) RevokeSession(context.Context, PersistenceID, PersistenceID) (bool, error) {
	return false, nil
}
func (*bootstrapPersistenceFake) Complete(context.Context, CompletionRequest) (CompletionResult, error) {
	return CompletionResult{}, nil
}
func (f *bootstrapPersistenceFake) Cleanup(context.Context) (CleanupResult, error) {
	f.cleanupCalls++
	return CleanupResult{}, f.cleanupErr
}

func TestDecodeBootstrapTokenCanonicalOnly(t *testing.T) {
	valid := tokenString(1)
	for _, tt := range []struct {
		name  string
		value string
		valid bool
	}{
		{name: "valid", value: valid, valid: true},
		{name: "empty"},
		{name: "short", value: valid[:42]},
		{name: "padded", value: valid + "="},
		{name: "standard alphabet", value: valid[:42] + "/"},
		{name: "whitespace", value: " " + valid},
		{name: "noncanonical bits", value: valid[:42] + "B"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeBootstrapToken(tt.value)
			if (err == nil) != tt.valid {
				t.Fatalf("DecodeBootstrapToken() error = %v, valid = %t", err, tt.valid)
			}
		})
	}
}

func TestKeyDerivationMatchesD015AndSeparatesDomains(t *testing.T) {
	token, err := DecodeBootstrapToken(tokenString(3))
	if err != nil {
		t.Fatal(err)
	}
	keyMAC := hmac.New(sha256.New, token[:])
	_, _ = keyMAC.Write([]byte("requestarr/setup/v1/key"))
	var wantKey Digest
	copy(wantKey[:], keyMAC.Sum(nil))

	service := NewBootstrapService(&bootstrapPersistenceFake{}, token, true)
	if service.key != wantKey {
		t.Fatal("setup key does not match D015 derivation")
	}
	verifierMAC := hmac.New(sha256.New, wantKey[:])
	_, _ = verifierMAC.Write(append([]byte("requestarr/setup/v1/bootstrap-token\x00"), token[:]...))
	if !hmac.Equal(service.verifier[:], verifierMAC.Sum(nil)) {
		t.Fatal("bootstrap verifier does not match D015 derivation")
	}
	if service.verifier == sessionDigest(service.key, token) || service.verifier == peerDigest(service.key, netip.MustParseAddr("192.0.2.1")) {
		t.Fatal("domain-separated digests unexpectedly collide")
	}
}

func TestBootstrapServiceAcceptedCandidatesAndOpaquePersistence(t *testing.T) {
	configured, _ := DecodeBootstrapToken(tokenString(11))
	expires := time.Now().UTC().Add(SessionMaxAge)
	for _, tt := range []struct {
		name      string
		candidate string
		valid     bool
		outcome   ExchangeOutcome
		want      BootstrapOutcome
	}{
		{name: "valid", candidate: tokenString(11), valid: true, outcome: ExchangeSessionCreated, want: BootstrapSessionCreated},
		{name: "accepted-format invalid", candidate: tokenString(12), outcome: ExchangeTokenInvalid, want: BootstrapTokenInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &bootstrapPersistenceFake{result: ExchangeResult{Outcome: tt.outcome, ExpiresAt: expires}}
			random := bytes.Repeat([]byte{0x5a}, 96)
			service := NewBootstrapService(fake, configured, true).WithRandomSource(bytes.NewReader(random))
			result := service.Exchange(context.Background(), tt.candidate, netip.MustParseAddr("2001:db8::1"))
			if result.Outcome != tt.want || fake.request.Verify == nil || fake.request.Verify() != tt.valid {
				t.Fatalf("outcome = %v verifier result mismatch, want %v/%t", result.Outcome, tt.want, tt.valid)
			}
			if fake.request.PeerKey == (Digest{}) || fake.request.GlobalKey == (Digest{}) || fake.request.PeerKey == fake.request.GlobalKey {
				t.Fatal("rate keys are empty or not domain-separated")
			}
			if tt.valid {
				if len(result.Session) != BootstrapTokenSize || result.ExpiresAt != expires {
					t.Fatalf("session result = %+v", result)
				}
				rawSession, err := base64.RawURLEncoding.DecodeString(result.Session)
				if err != nil || len(rawSession) != 32 || !bytes.Equal(rawSession, random[:32]) {
					t.Fatalf("session is not opaque 32-byte random material")
				}
				if bytes.Contains(fake.material.SessionDigest[:], rawSession) || fake.material.SessionDigest == (Digest{}) {
					t.Fatal("persistence received raw or empty session material")
				}
			}
			for _, secret := range []string{tt.candidate, result.Session} {
				if secret != "" && (bytes.Contains(fake.request.PeerKey[:], []byte(secret)) || bytes.Contains(fake.request.GlobalKey[:], []byte(secret))) {
					t.Fatal("persistence key contains raw secret")
				}
			}
		})
	}
}

func TestBootstrapServiceLockedCompletedRateLimitedAndRandomFailure(t *testing.T) {
	token, _ := DecodeBootstrapToken(tokenString(21))
	peer := netip.MustParseAddr("127.0.0.1")
	tests := []struct {
		name       string
		configured bool
		persisted  ExchangeOutcome
		random     *bytes.Reader
		want       BootstrapOutcome
	}{
		{name: "locked", persisted: ExchangeSetupLocked, random: bytes.NewReader(nil), want: BootstrapSetupLocked},
		{name: "completed ignores random", configured: true, persisted: ExchangeSetupCompleted, random: bytes.NewReader(nil), want: BootstrapSetupCompleted},
		{name: "rate limited ignores random", configured: true, persisted: ExchangeRateLimited, random: bytes.NewReader(nil), want: BootstrapRateLimited},
		{name: "session random unavailable", configured: true, persisted: ExchangeSessionCreated, random: bytes.NewReader(nil), want: BootstrapUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &bootstrapPersistenceFake{result: ExchangeResult{Outcome: tt.persisted}}
			service := NewBootstrapService(fake, token, tt.configured).WithRandomSource(tt.random)
			got := service.Exchange(context.Background(), tokenString(21), peer)
			if got.Outcome != tt.want {
				t.Fatalf("outcome = %v, want %v", got.Outcome, tt.want)
			}
			if !tt.configured && !fake.request.Locked {
				t.Fatal("missing configured token did not propagate locked policy")
			}
		})
	}
}

func TestOriginNormalizationAndDirectPeerBoundary(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "HTTPS://Requestarr.Example", want: "https://requestarr.example:443"},
		{input: "http://LOCALHOST:8080", want: "http://localhost:8080"},
		{input: "http://[::1]", want: "http://[::1]:80"},
	} {
		got, err := NormalizeOrigin(tt.input)
		if err != nil || got != tt.want {
			t.Fatalf("NormalizeOrigin(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
		}
	}
	for _, invalid := range []string{"null", "https://example.com/path", "https://user@example.com", "https://example.com?x=1", "ftp://example.com"} {
		if _, err := NormalizeOrigin(invalid); !errors.Is(err, ErrOrigin) {
			t.Fatalf("NormalizeOrigin(%q) error = %v", invalid, err)
		}
	}
	for _, tt := range []struct {
		remote string
		want   string
	}{
		{remote: "192.0.2.10:4567", want: "192.0.2.10"},
		{remote: "[2001:db8::10]:4567", want: "2001:db8::10"},
		{remote: "[::ffff:192.0.2.11]:4567", want: "192.0.2.11"},
	} {
		got, err := DirectPeerIP(tt.remote)
		if err != nil || got.String() != tt.want {
			t.Fatalf("DirectPeerIP(%q) = %q, %v; want %q", tt.remote, got, err, tt.want)
		}
	}
	for _, invalid := range []string{"192.0.2.1", "client.example:1234", "bad"} {
		if _, err := DirectPeerIP(invalid); !errors.Is(err, ErrPeerAddress) {
			t.Fatalf("DirectPeerIP(%q) error = %v", invalid, err)
		}
	}
}

func TestSuccessfulExchangeCleanupIsThrottled(t *testing.T) {
	token, _ := DecodeBootstrapToken(tokenString(31))
	fake := &bootstrapPersistenceFake{result: ExchangeResult{Outcome: ExchangeSessionCreated, ExpiresAt: time.Now().Add(SessionMaxAge)}}
	service := NewBootstrapService(fake, token, true).WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{1}, 192)))
	now := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return now }
	for range 2 {
		if got := service.Exchange(context.Background(), tokenString(31), netip.MustParseAddr("127.0.0.1")); got.Outcome != BootstrapSessionCreated {
			t.Fatalf("exchange outcome = %v", got.Outcome)
		}
	}
	if fake.cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", fake.cleanupCalls)
	}
}

func TestCleanupFailureDoesNotFailCommittedExchange(t *testing.T) {
	token, _ := DecodeBootstrapToken(tokenString(41))
	fake := &bootstrapPersistenceFake{
		result:     ExchangeResult{Outcome: ExchangeSessionCreated, ExpiresAt: time.Now().Add(SessionMaxAge)},
		cleanupErr: errors.New("database-secret-detail"),
	}
	called := 0
	service := NewBootstrapService(fake, token, true).
		WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{2}, 96))).
		WithCleanupFailureHandler(func() { called++ })
	result := service.Exchange(context.Background(), tokenString(41), netip.MustParseAddr("127.0.0.1"))
	if result.Outcome != BootstrapSessionCreated || called != 1 {
		t.Fatalf("exchange outcome=%v cleanup callback=%d", result.Outcome, called)
	}
}

func tokenString(seed byte) string {
	value := bytes.Repeat([]byte{seed}, 32)
	return base64.RawURLEncoding.EncodeToString(value)
}

func TestSecretValuesAreNotRenderedBySetupErrors(t *testing.T) {
	secret := tokenString(99)
	if _, err := DecodeBootstrapToken(strings.Repeat("x", 42)); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("format error exposed secret: %v", err)
	}
}
