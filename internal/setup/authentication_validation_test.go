package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"git.example.com/evilperson1337/Requestarr/internal/auth/oidc"
)

type validationPersistenceFake struct {
	order    *[]string
	snapshot AuthenticationValidationSnapshot
	result   AuthenticationValidationResult
	session  Digest
	global   Digest
	revision int64
	evidence *oidc.Evidence
	failure  oidc.FailureKind
}

func (f *validationPersistenceFake) ReserveAuthenticationValidation(_ context.Context, session, global Digest, revision int64) (AuthenticationValidationSnapshot, error) {
	*f.order = append(*f.order, "reserve")
	f.session, f.global, f.revision = session, global, revision
	return f.snapshot, nil
}

func (f *validationPersistenceFake) FinishAuthenticationValidation(_ context.Context, session Digest, snapshot AuthenticationValidationSnapshot, evidence *oidc.Evidence, failure oidc.FailureKind) (AuthenticationValidationResult, error) {
	*f.order = append(*f.order, "finish")
	f.session, f.evidence, f.failure = session, evidence, failure
	return f.result, nil
}

type discovererFake struct {
	order    *[]string
	input    oidc.DiscoveryInput
	evidence oidc.Evidence
	err      error
}

func (f *discovererFake) Discover(_ context.Context, input oidc.DiscoveryInput) (oidc.Evidence, error) {
	*f.order = append(*f.order, "discover")
	f.input = input
	return f.evidence, f.err
}

func TestAuthenticationValidationServiceOrdersTransactionsAroundDiscovery(t *testing.T) {
	order := []string{}
	authentication := Authentication{Issuer: "https://issuer.example", ClientType: ClientTypeConfidential}
	evidence := oidc.Evidence{PolicyVersion: oidc.PolicyVersion, Issuer: authentication.Issuer, MetadataDigest: string(bytes.Repeat([]byte{'a'}, 64))}
	persistence := &validationPersistenceFake{order: &order,
		snapshot: AuthenticationValidationSnapshot{Outcome: AuthenticationValidationSucceeded, Authentication: authentication, Revision: 7},
		result:   AuthenticationValidationResult{Outcome: AuthenticationValidationSucceeded, Revision: 7}}
	discovery := &discovererFake{order: &order, evidence: evidence}
	service := NewAuthenticationValidationService(persistence, discovery, [32]byte{1}, true)
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	result := service.Validate(context.Background(), raw, 7)
	if result.Outcome != AuthenticationValidationSucceeded || stringJoin(order) != "reserve,discover,finish" {
		t.Fatalf("result=%+v order=%v", result, order)
	}
	if discovery.input != (oidc.DiscoveryInput{Issuer: authentication.Issuer, ClientType: oidc.ClientConfidential}) {
		t.Fatalf("discovery input = %+v", discovery.input)
	}
	if persistence.session == (Digest{}) || persistence.global == (Digest{}) || persistence.session == persistence.global {
		t.Fatal("rate/session digests were not distinct keyed values")
	}
	if persistence.evidence == nil || persistence.evidence.MetadataDigest != evidence.MetadataDigest {
		t.Fatalf("finish evidence = %+v", persistence.evidence)
	}
}

func TestAuthenticationValidationServiceShortCircuitsAndMapsSafeFailure(t *testing.T) {
	tests := []struct {
		name         string
		snapshot     AuthenticationValidationSnapshot
		discoveryErr error
		finish       AuthenticationValidationResult
		want         AuthenticationValidationOutcome
		wantOrder    string
		wantFailure  oidc.FailureKind
	}{
		{name: "stale reservation", snapshot: AuthenticationValidationSnapshot{Outcome: AuthenticationValidationStale}, want: AuthenticationValidationStale, wantOrder: "reserve"},
		{name: "rate limited", snapshot: AuthenticationValidationSnapshot{Outcome: AuthenticationValidationRateLimited}, want: AuthenticationValidationRateLimited, wantOrder: "reserve"},
		{name: "destination failure", snapshot: AuthenticationValidationSnapshot{Outcome: AuthenticationValidationSucceeded, Authentication: Authentication{Issuer: "https://issuer.example", ClientType: ClientTypePublic}, Revision: 1}, discoveryErr: &oidc.Failure{Kind: oidc.FailureDestination}, finish: AuthenticationValidationResult{Outcome: AuthenticationValidationDestination}, want: AuthenticationValidationDestination, wantOrder: "reserve,discover,finish", wantFailure: oidc.FailureDestination},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := []string{}
			persistence := &validationPersistenceFake{order: &order, snapshot: tt.snapshot, result: tt.finish}
			discovery := &discovererFake{order: &order, err: tt.discoveryErr}
			service := NewAuthenticationValidationService(persistence, discovery, [32]byte{1}, true)
			raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
			result := service.Validate(context.Background(), raw, 1)
			if result.Outcome != tt.want || stringJoin(order) != tt.wantOrder || persistence.failure != tt.wantFailure {
				t.Fatalf("result=%+v order=%v failure=%v", result, order, persistence.failure)
			}
		})
	}
}

func TestAuthenticationValidationServiceRejectsBoundaryInputsWithoutCalls(t *testing.T) {
	order := []string{}
	persistence := &validationPersistenceFake{order: &order}
	discovery := &discovererFake{order: &order}
	service := NewAuthenticationValidationService(persistence, discovery, [32]byte{1}, true)
	if got := service.Validate(context.Background(), "invalid", 0); got.Outcome != AuthenticationValidationRequired {
		t.Fatalf("revision outcome = %v", got.Outcome)
	}
	if got := service.Validate(context.Background(), "invalid", 1); got.Outcome != AuthenticationValidationSessionRequired {
		t.Fatalf("session outcome = %v", got.Outcome)
	}
	discovery.err = errors.New("unsafe provider detail https://secret.example")
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	persistence.snapshot = AuthenticationValidationSnapshot{Outcome: AuthenticationValidationSucceeded, Authentication: Authentication{Issuer: "https://issuer.example", ClientType: ClientTypePublic}, Revision: 1}
	if got := service.Validate(context.Background(), raw, 1); got.Outcome != AuthenticationValidationUnavailable {
		t.Fatalf("untyped discovery outcome = %v", got.Outcome)
	}
}

func stringJoin(values []string) string {
	var result string
	for i, value := range values {
		if i > 0 {
			result += ","
		}
		result += value
	}
	return result
}
