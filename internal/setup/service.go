package setup

import (
	"context"
	"fmt"
	"time"
)

type Repository interface {
	LoadSetupDraft(context.Context, Draft, string) (Draft, error)
	SaveSetupDraft(context.Context, string, int64, Step, Settings) (Draft, error)
	RecordSetupValidation(context.Context, string, int64, Validation) (Draft, error)
}

type Checker interface {
	Check(context.Context, Target, Settings) (CheckCode, error)
}

type Service struct {
	repo    Repository
	checker Checker
	runtime Runtime
}

func NewService(repo Repository, checker Checker, runtime Runtime) *Service {
	return &Service{repo: repo, checker: checker, runtime: runtime}
}

func (s *Service) initial() Draft {
	return Draft{Revision: 1, CurrentStep: StepGeneral, Inactive: true, Validations: []Validation{}, Settings: Settings{
		General:        GeneralSettings{InstanceName: "Requestarr", Timezone: "UTC", PublicURL: s.runtime.PublicURL},
		Authentication: AuthenticationSettings{Issuer: s.runtime.OIDCIssuer, ClientID: s.runtime.OIDCClientID, ClientSecretEnv: OIDCSecretEnv},
		Providers:      ProviderSettings{TMDBEnabled: true, TMDBTokenEnv: TMDBTokenEnv, MusicBrainzEnabled: true, IGDBEnabled: true, IGDBClientID: s.runtime.IGDBClientID, IGDBClientSecretEnv: IGDBSecretEnv},
	}}
}

func (s *Service) decorate(d Draft) Draft {
	d.Inactive = true
	if d.Validations == nil {
		d.Validations = []Validation{}
	}
	d.Environment = []EnvironmentItem{
		{Name: "REQUESTARR_PUBLIC_URL", Configured: s.runtime.PublicURL != "", NeedsRestart: d.Settings.General.PublicURL != s.runtime.PublicURL, DesiredValue: d.Settings.General.PublicURL},
		{Name: "REQUESTARR_OIDC_ISSUER", Configured: s.runtime.OIDCIssuer != "", NeedsRestart: d.Settings.Authentication.Issuer != s.runtime.OIDCIssuer, DesiredValue: d.Settings.Authentication.Issuer},
		{Name: "REQUESTARR_OIDC_CLIENT_ID", Configured: s.runtime.OIDCClientID != "", NeedsRestart: d.Settings.Authentication.ClientID != s.runtime.OIDCClientID, DesiredValue: d.Settings.Authentication.ClientID},
		{Name: OIDCSecretEnv, Secret: true, Configured: s.runtime.OIDCSecretConfigured, NeedsRestart: !s.runtime.OIDCSecretConfigured || d.Settings.Authentication.Issuer != s.runtime.OIDCIssuer || d.Settings.Authentication.ClientID != s.runtime.OIDCClientID},
	}
	if d.Settings.Providers.TMDBEnabled {
		d.Environment = append(d.Environment, EnvironmentItem{Name: TMDBTokenEnv, Secret: true, Configured: s.runtime.TMDBTokenConfigured, NeedsRestart: !s.runtime.TMDBTokenConfigured})
	}
	if d.Settings.Providers.IGDBEnabled {
		d.Environment = append(d.Environment,
			EnvironmentItem{Name: "REQUESTARR_IGDB_CLIENT_ID", Configured: s.runtime.IGDBClientID != "", NeedsRestart: d.Settings.Providers.IGDBClientID != s.runtime.IGDBClientID, DesiredValue: d.Settings.Providers.IGDBClientID},
			EnvironmentItem{Name: IGDBSecretEnv, Secret: true, Configured: s.runtime.IGDBSecretConfigured, NeedsRestart: !s.runtime.IGDBSecretConfigured || d.Settings.Providers.IGDBClientID != s.runtime.IGDBClientID})
	}
	return d
}

func requireAdmin(actor Actor) error {
	if actor.ID == "" || actor.Role != "admin" {
		return ErrForbidden
	}
	return nil
}

func (s *Service) Load(ctx context.Context, actor Actor) (Draft, error) {
	if err := requireAdmin(actor); err != nil {
		return Draft{}, err
	}
	d, err := s.repo.LoadSetupDraft(ctx, s.initial(), actor.ID)
	if err != nil {
		return Draft{}, fmt.Errorf("load setup draft: %w", err)
	}
	return s.decorate(d), nil
}

func (s *Service) Save(ctx context.Context, actor Actor, input SaveInput) (Draft, error) {
	if err := requireAdmin(actor); err != nil {
		return Draft{}, err
	}
	if input.ExpectedRevision < 1 || (input.CurrentStep != StepGeneral && input.CurrentStep != StepAuthentication && input.CurrentStep != StepProviders && input.CurrentStep != StepLibraries) {
		return Draft{}, ErrInvalid
	}
	if err := input.Settings.Validate(s.runtime.Development); err != nil {
		return Draft{}, err
	}
	d, err := s.repo.SaveSetupDraft(ctx, actor.ID, input.ExpectedRevision, input.CurrentStep, input.Settings)
	if err != nil {
		return Draft{}, fmt.Errorf("save setup draft: %w", err)
	}
	return s.decorate(d), nil
}

func (s *Service) Validate(ctx context.Context, actor Actor, revision int64, target Target) (Draft, error) {
	if err := requireAdmin(actor); err != nil {
		return Draft{}, err
	}
	if revision < 1 || !target.Valid() {
		return Draft{}, ErrInvalid
	}
	d, err := s.Load(ctx, actor)
	if err != nil {
		return Draft{}, err
	}
	if d.Revision != revision {
		return Draft{}, ErrConflict
	}
	code, checkErr := s.checker.Check(ctx, target, d.Settings)
	if err := ctx.Err(); err != nil {
		return Draft{}, err
	}
	if checkErr != nil {
		code = CheckUnavailable
	}
	message := "Connection check failed. Verify the service address, network access, and server environment, then retry."
	switch code {
	case CheckOK:
		message = "Connection succeeded. Draft settings remain inactive."
	case CheckDisabled:
		message = "Provider is disabled in this draft."
	case CheckEnvironment:
		message = "Set the listed environment variables and restart Requestarr before checking this connection."
	case CheckIncomplete:
		message = "Complete this step's required non-secret fields before checking the connection."
	default:
		code = CheckUnavailable
	}
	now := time.Now().UTC()
	result := Validation{Target: target, Revision: revision, Valid: code == CheckOK || code == CheckDisabled, Code: code, Message: message, CheckedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	d, err = s.repo.RecordSetupValidation(ctx, actor.ID, revision, result)
	if err != nil {
		return Draft{}, fmt.Errorf("record setup validation: %w", err)
	}
	return s.decorate(d), nil
}
