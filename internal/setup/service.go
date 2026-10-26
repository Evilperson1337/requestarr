package setup

import (
	"context"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
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
	secrets SecretSources
}

// UseSecrets lets connection-check messages name the secret source.
func (s *Service) UseSecrets(sources SecretSources) { s.secrets = sources }

// checkedSecrets maps a check target to the secret it verifies and the noun
// used for it in messages. Navidrome joins in its own ticket.
var checkedSecrets = map[Target]struct {
	name secrets.Name
	noun string
}{
	TargetTMDB:          {secrets.TMDBReadToken, "token"},
	TargetJellyfin:      {secrets.JellyfinAPIKey, "API key"},
	TargetPlex:          {secrets.PlexToken, "token"},
	TargetRomm:          {secrets.RommToken, "token"},
	TargetIGDB:          {secrets.IGDBClientSecret, "client secret"},
	TargetNotifications: {secrets.AppriseConfigKey, "configuration key"},
}

// secretNote names the source of the secret a check just used, so an admin
// knows which value was verified. It never includes the value.
func (s *Service) secretNote(ctx context.Context, target Target, code CheckCode) string {
	checked, ok := checkedSecrets[target]
	if s.secrets == nil || !ok || code == CheckDisabled || code == CheckIncomplete {
		return ""
	}
	r, err := s.secrets.Resolve(ctx, checked.name)
	if err != nil {
		return ""
	}
	switch r.Source {
	case secrets.SourceEnvironment:
		return " Checked the " + checked.noun + " from the environment."
	case secrets.SourceStored:
		return " Checked the stored " + checked.noun + ", which a restart will use."
	case secrets.SourceShadowed:
		return " Checked the " + checked.noun + " from the environment; the stored " + checked.noun + " is shadowed by environment and is not used."
	case secrets.SourceUnreadable:
		return " The stored " + checked.noun + " cannot be read with the current REQUESTARR_SECRETS_KEY; enter it again or restore the key."
	}
	return ""
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
		d.Environment = append(d.Environment, EnvironmentItem{Name: TMDBTokenEnv, Secret: true, Configured: s.runtime.TMDBTokenConfigured, NeedsRestart: !s.runtime.TMDBTokenConfigured})
	}
	if d.Settings.Providers.IGDBEnabled {
		d.Environment = append(d.Environment,
			EnvironmentItem{Name: "REQUESTARR_IGDB_CLIENT_ID", Configured: s.runtime.IGDBClientID != "", NeedsRestart: d.Settings.Providers.IGDBClientID != s.runtime.IGDBClientID, DesiredValue: d.Settings.Providers.IGDBClientID},
			EnvironmentItem{Name: IGDBSecretEnv, Secret: true, Configured: s.runtime.IGDBSecretConfigured, NeedsRestart: !s.runtime.IGDBSecretConfigured || d.Settings.Providers.IGDBClientID != s.runtime.IGDBClientID})
	}
	if n := d.Settings.Notifications; n.AppriseEnabled || s.runtime.AppriseURL != "" {
		d.Environment = append(d.Environment,
			EnvironmentItem{Name: "REQUESTARR_APPRISE_URL", Configured: s.runtime.AppriseURL != "", NeedsRestart: n.AppriseURL != s.runtime.AppriseURL, DesiredValue: n.AppriseURL},
			EnvironmentItem{Name: AppriseConfigKeyEnv, Secret: true, Configured: s.runtime.AppriseKeyConfigured, NeedsRestart: n.AppriseEnabled && !s.runtime.AppriseKeyConfigured})
	}
	for _, entry := range []struct {
		kind   string
		source SourceSettings
	}{
		{"jellyfin", d.Settings.Libraries.Jellyfin}, {"plex", d.Settings.Libraries.Plex},
		{"romm", d.Settings.Libraries.Romm}, {"navidrome", d.Settings.Libraries.Navidrome},
	} {
		kind, source := entry.kind, entry.source
		var url, secretEnv string
		switch kind {
		case "jellyfin":
			url, secretEnv = "REQUESTARR_JELLYFIN_URL", source.TokenEnv
		case "plex":
			url, secretEnv = "REQUESTARR_PLEX_URL", source.TokenEnv
		case "romm":
			url, secretEnv = "REQUESTARR_ROMM_URL", source.TokenEnv
		case "navidrome":
			url, secretEnv = "REQUESTARR_NAVIDROME_URL", source.PasswordEnv
		}
		activeURL := s.runtime.LibraryURLs[kind]
		activeTypes := defaultSourceMediaTypes(kind)
		needsRestart := source.URL != activeURL || source.Enabled != (activeURL != "") || source.Enabled && !sameMediaTypes(source.MediaTypes, activeTypes)
		d.Environment = append(d.Environment, EnvironmentItem{Name: url, Configured: activeURL != "", NeedsRestart: needsRestart, DesiredValue: source.URL})
		if kind == "navidrome" {
			d.Environment = append(d.Environment, EnvironmentItem{Name: source.UsernameEnv, Secret: true, Configured: s.runtime.LibrarySecrets[kind+"Username"], NeedsRestart: source.Enabled && !s.runtime.LibrarySecrets[kind+"Username"]}, EnvironmentItem{Name: secretEnv, Secret: true, Configured: s.runtime.LibrarySecrets[kind+"Password"], NeedsRestart: source.Enabled && !s.runtime.LibrarySecrets[kind+"Password"]})
		} else {
			d.Environment = append(d.Environment, EnvironmentItem{Name: secretEnv, Secret: true, Configured: s.runtime.LibrarySecrets[kind], NeedsRestart: source.Enabled && !s.runtime.LibrarySecrets[kind]})
		}
	}
	return d
}

func defaultNotifications(runtime Runtime) NotificationSettings {
	return NotificationSettings{AppriseEnabled: runtime.AppriseURL != "", AppriseURL: runtime.AppriseURL, AppriseConfigKeyEnv: AppriseConfigKeyEnv}
}

func defaultLibraries(runtime Runtime) LibrariesSettings {
	return LibrariesSettings{
		Jellyfin:  SourceSettings{Enabled: runtime.LibraryURLs["jellyfin"] != "", URL: runtime.LibraryURLs["jellyfin"], MediaTypes: defaultSourceMediaTypes("jellyfin"), TokenEnv: JellyfinAPIKeyEnv},
		Plex:      SourceSettings{Enabled: runtime.LibraryURLs["plex"] != "", URL: runtime.LibraryURLs["plex"], MediaTypes: defaultSourceMediaTypes("plex"), TokenEnv: PlexTokenEnv},
		Romm:      SourceSettings{Enabled: runtime.LibraryURLs["romm"] != "", URL: runtime.LibraryURLs["romm"], MediaTypes: defaultSourceMediaTypes("romm"), TokenEnv: RommTokenEnv},
		Navidrome: SourceSettings{Enabled: runtime.LibraryURLs["navidrome"] != "", URL: runtime.LibraryURLs["navidrome"], MediaTypes: defaultSourceMediaTypes("navidrome"), UsernameEnv: NavidromeUsernameEnv, PasswordEnv: NavidromePasswordEnv},
	}
}

func normalizeLibraries(l LibrariesSettings, runtime Runtime) LibrariesSettings {
	if emptySource(l.Jellyfin) && emptySource(l.Plex) && emptySource(l.Romm) && emptySource(l.Navidrome) {
		return defaultLibraries(runtime)
	}
	defaults := map[string]*SourceSettings{"jellyfin": &l.Jellyfin, "plex": &l.Plex, "romm": &l.Romm, "navidrome": &l.Navidrome}
	for kind, source := range defaults {
		if source.MediaTypes == nil {
			source.MediaTypes = []domain.MediaType{}
		}
		switch kind {
		case "jellyfin":
			if source.TokenEnv == "" {
				source.TokenEnv = JellyfinAPIKeyEnv
			}
		case "plex":
			if source.TokenEnv == "" {
				source.TokenEnv = PlexTokenEnv
			}
		case "romm":
			if source.TokenEnv == "" {
				source.TokenEnv = RommTokenEnv
			}
		case "navidrome":
			if source.UsernameEnv == "" {
				source.UsernameEnv = NavidromeUsernameEnv
			}
			if source.PasswordEnv == "" {
				source.PasswordEnv = NavidromePasswordEnv
			}
		}
	}
	return l
}

func emptySource(s SourceSettings) bool {
	return !s.Enabled && s.URL == "" && len(s.MediaTypes) == 0 && s.TokenEnv == "" && s.UsernameEnv == "" && s.PasswordEnv == ""
}

func defaultSourceMediaTypes(kind string) []domain.MediaType {
	switch kind {
	case "jellyfin":
		return []domain.MediaType{domain.MediaMovie, domain.MediaTV}
	case "plex":
		return []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong}
	case "romm":
		return []domain.MediaType{domain.MediaGame}
	case "navidrome":
		return []domain.MediaType{domain.MediaAlbum, domain.MediaSong}
	default:
		return nil
	}
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
	message += s.secretNote(ctx, target, code)
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
