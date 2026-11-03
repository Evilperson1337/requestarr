package setup

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

type memoryDrafts struct {
	draft Draft
	reads int
}

func (r *memoryDrafts) LoadSetupDraft(_ context.Context, initial Draft, _ string) (Draft, error) {
	r.reads++
	if r.draft.Revision == 0 {
		r.draft = initial
	}
	return r.draft, nil
}

func (r *memoryDrafts) CompleteSetupDraft(_ context.Context, _ string, expected int64) (Draft, error) {
	if expected != r.draft.Revision {
		return Draft{}, ErrConflict
	}
	now := time.Now()
	r.draft.CompletedAt = &now
	return r.draft, nil
}

func (r *memoryDrafts) SaveSetupDraft(_ context.Context, actor string, expected int64, step Step, settings Settings) (Draft, error) {
	if expected != r.draft.Revision {
		return Draft{}, ErrConflict
	}
	r.draft.Revision++
	r.draft.CurrentStep, r.draft.Settings = step, settings
	r.draft.Validations = nil
	return r.draft, nil
}

func (r *memoryDrafts) RecordSetupValidation(_ context.Context, actor string, expected int64, result Validation) (Draft, error) {
	if expected != r.draft.Revision {
		return Draft{}, ErrConflict
	}
	r.draft.Validations = append(r.draft.Validations, result)
	return r.draft, nil
}

type fakeChecker struct{ err error }

func (c fakeChecker) Check(context.Context, Target, Settings) (CheckCode, error) {
	return CheckOK, c.err
}

func (fakeChecker) Describe(_ context.Context, _ Target, code CheckCode) string {
	return "described: " + string(code)
}

// codeChecker returns a fixed code, whatever the target.
type codeChecker struct{ code CheckCode }

func (c codeChecker) Check(context.Context, Target, Settings) (CheckCode, error) { return c.code, nil }
func (c codeChecker) Describe(_ context.Context, _ Target, code CheckCode) string {
	return "described: " + string(code)
}

func TestDraftResumesAcrossServicesAndConnectionFailureKeepsSettings(t *testing.T) {
	ctx := context.Background()
	repo := &memoryDrafts{}
	runtime := Runtime{PublicURL: "https://requestarr.example", OIDCIssuer: "https://issuer.example", OIDCClientID: "requestarr"}
	actor := Actor{ID: "admin-1", Role: "admin"}
	first := NewService(repo, fakeChecker{}, runtime)
	draft, err := first.Load(ctx, actor)
	if err != nil || draft.Revision != 1 || draft.CurrentStep != StepGeneral {
		t.Fatalf("initial draft=%+v err=%v", draft, err)
	}
	settings := draft.Settings
	settings.General.InstanceName = "Shared library"
	saved, err := first.Save(ctx, actor, SaveInput{ExpectedRevision: draft.Revision, CurrentStep: StepAuthentication, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	second := NewService(repo, fakeChecker{err: errors.New("upstream error containing a secret")}, runtime)
	resumed, err := second.Load(ctx, actor)
	if err != nil || resumed.CurrentStep != StepAuthentication || resumed.Settings.General.InstanceName != "Shared library" || resumed.Revision != saved.Revision {
		t.Fatalf("resume=%+v err=%v", resumed, err)
	}
	checked, err := second.Validate(ctx, actor, resumed.Revision, TargetAuthentication)
	if err != nil || len(checked.Validations) != 1 || checked.Validations[0].Valid || checked.Validations[0].Code != CheckUnavailable {
		t.Fatalf("failed connection should produce a saved safe result: %+v err=%v", checked, err)
	}
	if checked.Validations[0].Message != "described: connection_failed" || strings.Contains(checked.Validations[0].Message, "upstream error") {
		t.Fatalf("unsafe or unclear error: %q", checked.Validations[0].Message)
	}
	if !reflect.DeepEqual(checked.Settings, resumed.Settings) || checked.CurrentStep != StepAuthentication {
		t.Fatal("failed validation discarded or advanced the draft")
	}
}

func TestSetupServiceRejectsNonAdminsAndUnsafeDraftFields(t *testing.T) {
	ctx := context.Background()
	repo := &memoryDrafts{}
	service := NewService(repo, fakeChecker{}, Runtime{PublicURL: "https://requestarr.example", OIDCIssuer: "https://issuer.example", OIDCClientID: "requestarr"})
	for _, actor := range []Actor{{}, {ID: "viewer-1", Role: "viewer"}, {ID: "requester-1", Role: "requester"}} {
		if _, err := service.Load(ctx, actor); !errors.Is(err, ErrForbidden) {
			t.Fatalf("load allowed %+v: %v", actor, err)
		}
		if _, err := service.Save(ctx, actor, SaveInput{}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("save allowed %+v: %v", actor, err)
		}
		if _, err := service.Validate(ctx, actor, 1, TargetGeneral); !errors.Is(err, ErrForbidden) {
			t.Fatalf("validate allowed %+v: %v", actor, err)
		}
	}
	if repo.reads != 0 {
		t.Fatal("non-admin reached draft storage")
	}
	actor := Actor{ID: "admin-1", Role: "admin"}
	draft, err := service.Load(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	storedSettings := repo.draft.Settings
	for _, tc := range []struct {
		name   string
		change func(*Settings)
	}{
		{"secret instead of environment name", func(s *Settings) { s.Authentication.ClientSecretEnv = "actual-secret-value" }},
		{"unknown environment variable", func(s *Settings) { s.Providers.TMDBTokenEnv = "DATABASE_PASSWORD" }},
		{"credential URL", func(s *Settings) { s.General.PublicURL = "https://user:secret@example.test" }},
		{"secret in issuer query", func(s *Settings) { s.Authentication.Issuer = "https://issuer.example?client_secret=secret" }},
		{"duplicate-valued fields retain individual bounds", func(s *Settings) {
			s.General.InstanceName = strings.Repeat("x", 200)
			s.Authentication.ClientID = s.General.InstanceName
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := draft.Settings
			tc.change(&settings)
			if _, err := service.Save(ctx, actor, SaveInput{ExpectedRevision: 1, CurrentStep: StepGeneral, Settings: settings}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsafe draft accepted: %v", err)
			}
			if repo.draft.Revision != 1 || !reflect.DeepEqual(repo.draft.Settings, storedSettings) {
				t.Fatal("rejected settings reached storage")
			}
		})
	}
}

func TestLibraryDraftRestrictsAuthorityToAdapterMediaTypes(t *testing.T) {
	settings := NewService(&memoryDrafts{}, fakeChecker{}, Runtime{}).initial().Settings
	settings.Libraries.Plex.Enabled = true
	settings.Libraries.Plex.URL = "https://plex.example.test"
	settings.Libraries.Plex.MediaTypes = []domain.MediaType{domain.MediaMovie, domain.MediaAlbum}
	if err := settings.Validate(false); err != nil {
		t.Fatalf("valid Plex types rejected: %v", err)
	}
	tests := []struct {
		name   string
		change func(*SourceSettings)
	}{
		{"unsupported type", func(s *SourceSettings) { s.MediaTypes = []domain.MediaType{domain.MediaGame} }},
		{"duplicate type", func(s *SourceSettings) { s.MediaTypes = []domain.MediaType{domain.MediaMovie, domain.MediaMovie} }},
		{"credential-bearing URL", func(s *SourceSettings) { s.URL = "https://user:secret@plex.example.test" }},
		{"path URL", func(s *SourceSettings) { s.URL = "https://plex.example.test/path" }},
		{"arbitrary credential reference", func(s *SourceSettings) { s.TokenEnv = "DATABASE_PASSWORD" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bad := settings
			tc.change(&bad.Libraries.Plex)
			if err := bad.Validate(false); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid library setting accepted: %v", err)
			}
		})
	}
}

func TestLibraryDraftPrefillsExistingSourcesWithoutReturningSecrets(t *testing.T) {
	runtime := Runtime{LibraryURLs: map[string]string{"plex": "https://plex.example.test"}, SecretsConfigured: map[secrets.Name]bool{secrets.PlexToken: true}}
	repo := &memoryDrafts{draft: Draft{Revision: 1, CurrentStep: StepLibraries, Settings: Settings{Libraries: LibrariesSettings{}}}}
	service := NewService(repo, fakeChecker{}, runtime)
	draft, err := service.Load(context.Background(), Actor{ID: "admin-1", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !draft.Settings.Libraries.Plex.Enabled || draft.Settings.Libraries.Plex.URL != runtime.LibraryURLs["plex"] || len(draft.Settings.Libraries.Plex.MediaTypes) != 4 {
		t.Fatalf("existing Plex configuration not represented: %+v", draft.Settings.Libraries.Plex)
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "plex-token") || strings.Contains(string(encoded), "secret-value") {
		t.Fatalf("runtime secret leaked in setup response: %s", encoded)
	}
}
