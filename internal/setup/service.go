package setup

import (
	"context"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
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
		Libraries:      defaultLibraries(s.runtime),
	}}
}

func (s *Service) decorate(d Draft) Draft {
	d.Inactive = true
	d.Settings.Libraries = normalizeLibraries(d.Settings.Libraries, s.runtime)
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
