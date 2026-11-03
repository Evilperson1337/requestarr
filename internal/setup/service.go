package setup

import (
	"context"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type Repository interface {
	LoadSetupDraft(context.Context, Draft, string) (Draft, error)
	SaveSetupDraft(context.Context, string, int64, Step, Settings) (Draft, error)
	RecordSetupValidation(context.Context, string, int64, Validation) (Draft, error)
	// CompleteSetupDraft marks the exact revision finished; it returns ErrConflict
	// for a stale revision and the completed draft unchanged if already finished.
	// SaveSetupDraft and RecordSetupValidation return ErrCompleted afterwards.
	CompleteSetupDraft(context.Context, string, int64) (Draft, error)
}

type Checker interface {
	Check(context.Context, Target, Settings) (CheckCode, error)
	// Describe is the message shown for a result, including which secret
	// source was verified. Wording is the checker's, not the service's.
	Describe(context.Context, Target, CheckCode) string
}

// SecretSources resolves the value the next restart would use for a secret and
// says where it comes from (D024). The value must never leave the server.
type SecretSources interface {
	Resolve(context.Context, secrets.Name) (secrets.Resolved, error)
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
		Libraries:      defaultLibraries(s.runtime),
		Notifications:  defaultNotifications(s.runtime),
		Policy:         PolicySettings{AutoApproveMediaTypes: []domain.MediaType{}},
	}}
}

func (s *Service) decorate(d Draft) Draft {
	d.Inactive = true
	d.Settings.Libraries = normalizeLibraries(d.Settings.Libraries, s.runtime)
	if d.Settings.Notifications == (NotificationSettings{}) {
		d.Settings.Notifications = defaultNotifications(s.runtime)
	}
	if d.Settings.Policy.AutoApproveMediaTypes == nil {
		d.Settings.Policy.AutoApproveMediaTypes = []domain.MediaType{}
	}
	d.Blockers = []Blocker{}
	if d.CompletedAt == nil {
		d.Blockers = d.pendingBlockers(time.Now())
	}
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
		d.Environment = append(d.Environment, EnvironmentItem{Name: TMDBTokenEnv, Secret: true, Configured: s.runtime.SecretConfigured(secrets.TMDBReadToken), NeedsRestart: !s.runtime.SecretConfigured(secrets.TMDBReadToken)})
	}
	if d.Settings.Providers.IGDBEnabled {
		// Without an operator override the saved ID applies at restart, not by
		// editing the environment. Do not misleadingly list it as missing env.
		if s.runtime.IGDBClientIDFromEnvironment {
			d.Environment = append(d.Environment, EnvironmentItem{Name: "REQUESTARR_IGDB_CLIENT_ID", Configured: true, NeedsRestart: d.Settings.Providers.IGDBClientID != s.runtime.IGDBClientID, DesiredValue: d.Settings.Providers.IGDBClientID})
		}
		d.Environment = append(d.Environment, EnvironmentItem{Name: IGDBSecretEnv, Secret: true, Configured: s.runtime.SecretConfigured(secrets.IGDBClientSecret), NeedsRestart: !s.runtime.SecretConfigured(secrets.IGDBClientSecret) || d.Settings.Providers.IGDBClientID != s.runtime.IGDBClientID})
	}
	if n := d.Settings.Notifications; n.AppriseEnabled || s.runtime.AppriseURL != "" {
		d.Environment = append(d.Environment,
			EnvironmentItem{Name: "REQUESTARR_APPRISE_URL", Configured: s.runtime.AppriseURL != "", NeedsRestart: n.AppriseURL != s.runtime.AppriseURL, DesiredValue: n.AppriseURL},
			EnvironmentItem{Name: AppriseConfigKeyEnv, Secret: true, Configured: s.runtime.SecretConfigured(secrets.AppriseConfigKey), NeedsRestart: n.AppriseEnabled && !s.runtime.SecretConfigured(secrets.AppriseConfigKey)})
	}
	for _, kind := range sources.All() {
		source := *d.Settings.Libraries.Source(kind.Slug)
		activeURL := s.runtime.LibraryURLs[kind.Slug]
		needsRestart := source.URL != activeURL || source.Enabled != (activeURL != "") || source.Enabled && !sameMediaTypes(source.MediaTypes, kind.MediaTypes)
		d.Environment = append(d.Environment, EnvironmentItem{Name: kind.URLEnv, Configured: activeURL != "", NeedsRestart: needsRestart, DesiredValue: source.URL})
		for _, credential := range kind.Credentials {
			if credential.Role == sources.RoleUsername {
				d.Environment = append(d.Environment, EnvironmentItem{Name: credential.Env, Configured: s.runtime.LibraryUsername(kind.Slug) != "", NeedsRestart: source.Enabled && source.Username != s.runtime.LibraryUsername(kind.Slug), DesiredValue: source.Username})
				continue
			}
			configured := s.runtime.SecretConfigured(secrets.Name(credential.Secret))
			d.Environment = append(d.Environment, EnvironmentItem{Name: credential.Env, Secret: true, Configured: configured, NeedsRestart: source.Enabled && !configured})
		}
	}
	return d
}

func defaultNotifications(runtime Runtime) NotificationSettings {
	return NotificationSettings{AppriseEnabled: runtime.AppriseURL != "", AppriseURL: runtime.AppriseURL, AppriseConfigKeyEnv: AppriseConfigKeyEnv}
}

func defaultLibraries(runtime Runtime) LibrariesSettings {
	var l LibrariesSettings
	for _, kind := range sources.All() {
		url := runtime.LibraryURLs[kind.Slug]
		source := SourceSettings{Enabled: url != "", URL: url, MediaTypes: kind.DefaultMediaTypes()}
		applyCredentialDefaults(&source, kind, runtime)
		*l.Source(kind.Slug) = source
	}
	return l
}

// applyCredentialDefaults fills the fixed environment names of the kind's
// credential fields, and the environment-configured username, where the draft
// leaves them empty.
func applyCredentialDefaults(source *SourceSettings, kind sources.Kind, runtime Runtime) {
	for _, credential := range kind.Credentials {
		switch credential.Role {
		case sources.RoleToken:
			if source.TokenEnv == "" {
				source.TokenEnv = credential.Env
			}
		case sources.RolePassword:
			if source.PasswordEnv == "" {
				source.PasswordEnv = credential.Env
			}
		case sources.RoleUsername:
			if source.Username == "" {
				source.Username = runtime.LibraryUsername(kind.Slug)
			}
		}
	}
}

func normalizeLibraries(l LibrariesSettings, runtime Runtime) LibrariesSettings {
	all := true
	for _, kind := range sources.All() {
		all = all && emptySource(*l.Source(kind.Slug))
	}
	if all {
		return defaultLibraries(runtime)
	}
	for _, kind := range sources.All() {
		source := l.Source(kind.Slug)
		if source.MediaTypes == nil {
			source.MediaTypes = []domain.MediaType{}
		}
		applyCredentialDefaults(source, kind, runtime)
	}
	return l
}

func emptySource(s SourceSettings) bool {
	return !s.Enabled && s.URL == "" && len(s.MediaTypes) == 0 && s.TokenEnv == "" && s.Username == "" && s.PasswordEnv == ""
}

func sameMediaTypes(a, b []domain.MediaType) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[domain.MediaType]bool, len(a))
	for _, media := range a {
		set[media] = true
	}
	for _, media := range b {
		if !set[media] {
			return false
		}
	}
	return true
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
	if input.ExpectedRevision < 1 || (input.CurrentStep != StepGeneral && input.CurrentStep != StepAuthentication && input.CurrentStep != StepProviders && input.CurrentStep != StepLibraries && input.CurrentStep != StepNotifications && input.CurrentStep != StepPolicy && input.CurrentStep != StepReview) {
		return Draft{}, ErrInvalid
	}
	if err := input.Settings.Validate(s.runtime.Development); err != nil {
		return Draft{}, err
	}
	if err := s.rejectCompleted(ctx, actor); err != nil {
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
	if d.CompletedAt != nil {
		return Draft{}, ErrCompleted
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
	switch code {
	case CheckOK, CheckDisabled, CheckEnvironment, CheckAuthentication, CheckIncomplete:
	default:
		code = CheckUnavailable
	}
	message := s.checker.Describe(ctx, target, code)
	now := time.Now().UTC()
	result := Validation{Target: target, Revision: revision, Valid: code == CheckOK || code == CheckDisabled, Code: code, Message: message, CheckedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	d, err = s.repo.RecordSetupValidation(ctx, actor.ID, revision, result)
	if err != nil {
		return Draft{}, fmt.Errorf("record setup validation: %w", err)
	}
	return s.decorate(d), nil
}

func (s *Service) rejectCompleted(ctx context.Context, actor Actor) error {
	d, err := s.Load(ctx, actor)
	if err != nil {
		return err
	}
	if d.CompletedAt != nil {
		return ErrCompleted
	}
	return nil
}

// Finish marks setup complete for the exact reviewed revision. It refuses while
// any required check is missing, failed or expired, and is idempotent once done.
func (s *Service) Finish(ctx context.Context, actor Actor, revision int64) (Draft, error) {
	if err := requireAdmin(actor); err != nil {
		return Draft{}, err
	}
	if revision < 1 {
		return Draft{}, ErrInvalid
	}
	d, err := s.Load(ctx, actor)
	if err != nil {
		return Draft{}, err
	}
	if d.CompletedAt != nil {
		return d, nil
	}
	if d.Revision != revision {
		return Draft{}, ErrConflict
	}
	if len(d.Blockers) > 0 {
		return d, ErrNotReady
	}
	d, err = s.repo.CompleteSetupDraft(ctx, actor.ID, revision)
	if err != nil {
		return Draft{}, fmt.Errorf("complete setup: %w", err)
	}
	return s.decorate(d), nil
}
