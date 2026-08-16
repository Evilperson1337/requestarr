package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

type workflowPersistenceFake struct {
	digest  Digest
	general General
	result  WorkflowResult
}

func (f *workflowPersistenceFake) ReadWorkflow(_ context.Context, digest Digest) (WorkflowResult, error) {
	f.digest = digest
	return f.result, nil
}

func (f *workflowPersistenceFake) SaveGeneral(_ context.Context, digest Digest, general General) (WorkflowResult, error) {
	f.digest, f.general = digest, general
	return f.result, nil
}

func (f *workflowPersistenceFake) SaveAuthentication(_ context.Context, digest Digest, _ Authentication, _ SecretOperation) (WorkflowResult, error) {
	f.digest = digest
	return f.result, nil
}

func TestNormalizeGeneral(t *testing.T) {
	got, err := NormalizeGeneral(General{InstanceName: " Requestarr ", PublicURL: "HTTPS://EXAMPLE.COM/", Timezone: "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	if got != (General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "America/New_York"}) {
		t.Fatalf("unexpected normalization: %#v", got)
	}
	for _, invalid := range []General{
		{},
		{InstanceName: "x", PublicURL: "https://user:secret@example.com", Timezone: "UTC"},
		{InstanceName: "x", PublicURL: "https://example.com/path", Timezone: "UTC"},
		{InstanceName: "x", PublicURL: "https://example.com", Timezone: "Local"},
		{InstanceName: "x", PublicURL: "https://example.com", Timezone: "not/a/zone"},
	} {
		if _, err := NormalizeGeneral(invalid); err == nil {
			t.Fatalf("expected invalid General value: %#v", invalid)
		}
	}
}

func TestNormalizeGeneralUsesCodePointLimitsWithByteBounds(t *testing.T) {
	validName := strings.Repeat("界", GeneralInstanceNameMax)
	got, err := NormalizeGeneral(General{InstanceName: validName, PublicURL: "https://example.com", Timezone: "UTC"})
	if err != nil || got.InstanceName != validName {
		t.Fatalf("code-point boundary rejected: %#v, %v", got, err)
	}
	for name, value := range map[string]General{
		"name code points": {InstanceName: validName + "界", PublicURL: "https://example.com", Timezone: "UTC"},
		"invalid UTF-8":    {InstanceName: string([]byte{0xff}), PublicURL: "https://example.com", Timezone: "UTC"},
		"URL query":        {InstanceName: "x", PublicURL: "https://example.com?secret=no", Timezone: "UTC"},
		"URL fragment":     {InstanceName: "x", PublicURL: "https://example.com/#part", Timezone: "UTC"},
		"URL relative":     {InstanceName: "x", PublicURL: "//example.com", Timezone: "UTC"},
		"URL non-HTTP":     {InstanceName: "x", PublicURL: "ftp://example.com", Timezone: "UTC"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeGeneral(value); err == nil {
				t.Fatalf("accepted invalid value: %#v", value)
			}
		})
	}
	if utf8.RuneCountInString(validName) != GeneralInstanceNameMax || len(validName) > GeneralInstanceNameBytesMax {
		t.Fatal("test boundary does not exercise declared limits")
	}
}

func TestWorkflowServiceDigestsSessionBeforePersistence(t *testing.T) {
	token := [32]byte{1}
	rawBytes := bytes.Repeat([]byte{2}, 32)
	raw := base64.RawURLEncoding.EncodeToString(rawBytes)
	fake := &workflowPersistenceFake{result: WorkflowResult{Outcome: WorkflowAvailable}}
	service := NewWorkflowService(fake, token, true)
	service.State(context.Background(), raw)
	if fake.digest == (Digest{}) || bytes.Contains(fake.digest[:], rawBytes) {
		t.Fatal("persistence did not receive only a keyed digest")
	}
	service.PutGeneral(context.Background(), raw, General{InstanceName: " x ", PublicURL: "https://example.com/", Timezone: "UTC"})
	if fake.general != (General{InstanceName: "x", PublicURL: "https://example.com", Timezone: "UTC"}) {
		t.Fatalf("unexpected persisted value: %#v", fake.general)
	}
}

func TestWorkflowServiceRejectsInvalidSessionAndGeneralWithoutPersistence(t *testing.T) {
	fake := &workflowPersistenceFake{}
	service := NewWorkflowService(fake, [32]byte{1}, true)
	if got := service.State(context.Background(), "raw-secret"); got.Outcome != WorkflowSessionRequired {
		t.Fatalf("unexpected outcome: %v", got.Outcome)
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	if got := service.PutGeneral(context.Background(), raw, General{}); got.Outcome != WorkflowInvalid {
		t.Fatalf("unexpected outcome: %v", got.Outcome)
	}
}

func TestWorkflowServiceValidationPrecedesSessionAuthorization(t *testing.T) {
	fake := &workflowPersistenceFake{}
	service := NewWorkflowService(fake, [32]byte{1}, true)
	if got := service.PutGeneral(context.Background(), "malformed-session", General{}); got.Outcome != WorkflowInvalid {
		t.Fatalf("outcome = %v, want validation failure", got.Outcome)
	}
	if fake.digest != (Digest{}) {
		t.Fatal("persistence invoked for invalid values")
	}
}

func TestWorkflowDraftStrictVersionedRoundTrip(t *testing.T) {
	general := General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"}
	encoded, err := EncodeWorkflowDraft(WorkflowDraft{General: &general})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWorkflowDraft(encoded)
	if err != nil || decoded.General == nil || *decoded.General != general {
		t.Fatalf("round trip failed: %#v %v", decoded, err)
	}
	for _, malformed := range []string{`{"version":3,"general":{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}}`, `{"version":1,"secret":"value"}`, `[]`, `null`} {
		if _, err := DecodeWorkflowDraft([]byte(malformed)); err == nil {
			t.Fatalf("accepted malformed draft: %s", malformed)
		}
	}
}

func TestNormalizeAuthenticationAndAggregateDraft(t *testing.T) {
	authentication, err := NormalizeAuthentication(Authentication{
		Issuer: " HTTPS://ID.EXAMPLE.COM/realms/main/ ", ClientID: " requestarr ", ClientType: ClientTypeConfidential,
		Scopes: []string{"openid", "profile"}, GroupsClaim: " groups ", DefaultRole: "viewer",
		GroupRoles: []GroupRoleMapping{{Group: " requesters ", Role: "requester"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if authentication.Issuer != "https://id.example.com/realms/main" || authentication.ClientID != "requestarr" || authentication.GroupRoles[0].Group != "requesters" {
		t.Fatalf("unexpected normalization: %#v", authentication)
	}
	general := General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"}
	encoded, err := EncodeWorkflowDraft(WorkflowDraft{General: &general, Authentication: &authentication})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("client_secret")) {
		t.Fatalf("draft contains secret state: %s", encoded)
	}
	decoded, err := DecodeWorkflowDraft(encoded)
	if err != nil || decoded.General == nil || decoded.Authentication == nil || !AuthenticationEqual(*decoded.Authentication, authentication) {
		t.Fatalf("aggregate round trip failed: %#v %v", decoded, err)
	}
	for _, invalid := range []Authentication{
		{Issuer: "https://id.example.com", ClientID: "client", Scopes: []string{"profile"}, GroupsClaim: "groups", DefaultRole: "viewer"},
		{Issuer: "https://user:secret@id.example.com", ClientID: "client", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"},
		{Issuer: "https://id.example.com?x=1", ClientID: "client", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"},
		{Issuer: "https://id.example.com", ClientID: "client", Scopes: []string{"openid", "openid"}, GroupsClaim: "groups", DefaultRole: "viewer"},
		{Issuer: "https://id.example.com", ClientID: "client", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "owner"},
	} {
		if _, err := NormalizeAuthentication(invalid); err == nil {
			t.Fatalf("accepted invalid Authentication: %#v", invalid)
		}
	}
}

func TestSecretOperationDoesNotFormatOrExposePlaintext(t *testing.T) {
	operation, err := ReplaceSecret("distinctive-secret")
	if err != nil {
		t.Fatal(err)
	}
	secret := "distinctive-secret"
	decimal := fmt.Sprint([]byte(secret))
	hex := fmt.Sprintf("%x", []byte(secret))
	for _, rendered := range []string{
		fmt.Sprint(operation), fmt.Sprintf("%s", operation), fmt.Sprintf("%q", operation),
		fmt.Sprintf("%v", operation), fmt.Sprintf("%+v", operation), fmt.Sprintf("%#v", operation),
		fmt.Sprintf("%x", operation), fmt.Sprintf("%X", operation), fmt.Sprintf("%d", operation),
		fmt.Sprintf("%v", struct{ Secret SecretOperation }{operation}),
	} {
		if strings.Contains(rendered, secret) || strings.Contains(rendered, decimal) || strings.Contains(rendered, hex) || strings.Contains(rendered, "100 105 115") {
			t.Fatalf("secret operation formatting exposed reconstructable plaintext: %q", rendered)
		}
	}
	if !operation.PlaintextEqual([]byte("distinctive-secret")) || operation.PlaintextEqual([]byte("other")) {
		t.Fatal("secret constant-time comparison returned an unexpected result")
	}
}

func TestNormalizeAuthenticationIssuerEscaping(t *testing.T) {
	base := Authentication{ClientID: "client", ClientType: ClientTypePublic, Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"}
	valid := map[string]string{
		"https://id.example.com/realm%20name/": "https://id.example.com/realm%20name",
		"https://id.example.com/%E2%98%83/":    "https://id.example.com/%E2%98%83",
		"https://id.example.com/realm%41/":     "https://id.example.com/realm%41",
	}
	for input, want := range valid {
		candidate := base
		candidate.Issuer = input
		got, err := NormalizeAuthentication(candidate)
		if err != nil || got.Issuer != want {
			t.Errorf("NormalizeAuthentication(%q) = %q, %v; want %q", input, got.Issuer, err, want)
		}
	}
	for _, input := range []string{
		"https://id.example.com/a%2Fb", "https://id.example.com/a%5Cb",
		"https://id.example.com/./realm", "https://id.example.com/%2e%2e/realm",
	} {
		candidate := base
		candidate.Issuer = input
		if _, err := NormalizeAuthentication(candidate); err == nil {
			t.Errorf("accepted ambiguous issuer %q", input)
		}
	}
}

func TestDecodeWorkflowDraftRejectsRecursiveDuplicateKeys(t *testing.T) {
	for _, document := range []string{
		`{"version":2,"version":2,"general":{"instance_name":"R","public_url":"https://example.com","timezone":"UTC"}}`,
		`{"version":2,"authentication":{"issuer":"https://id.example.com","issuer":"https://evil.example.com","client_id":"c","client_type":"public","scopes":["openid"],"groups_claim":"groups","default_role":"viewer","group_role_mappings":[]}}`,
		`{"version":2,"authentication":{"issuer":"https://id.example.com","client_id":"c","client_type":"public","scopes":["openid"],"groups_claim":"groups","default_role":"viewer","group_role_mappings":[{"group":"a","group":"b","role":"viewer"}]}}`,
	} {
		if _, err := DecodeWorkflowDraft([]byte(document)); err == nil {
			t.Fatalf("accepted duplicate-key draft: %s", document)
		}
	}
}
