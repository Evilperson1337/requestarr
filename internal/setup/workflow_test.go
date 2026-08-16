package setup

import (
	"bytes"
	"context"
	"encoding/base64"
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
	encoded, err := EncodeWorkflowDraft(general)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWorkflowDraft(encoded)
	if err != nil || decoded == nil || *decoded != general {
		t.Fatalf("round trip failed: %#v %v", decoded, err)
	}
	for _, malformed := range []string{`{"version":2,"general":{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}}`, `{"version":1,"secret":"value"}`, `[]`, `null`} {
		if _, err := DecodeWorkflowDraft([]byte(malformed)); err == nil {
			t.Fatalf("accepted malformed draft: %s", malformed)
		}
	}
}
