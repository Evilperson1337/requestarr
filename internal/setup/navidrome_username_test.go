package setup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

// A draft saved before the username became a plain field carries usernameEnv.
const legacyNavidromeLibraries = `{"jellyfin":{"enabled":false,"url":"","mediaTypes":[],"tokenEnv":"REQUESTARR_JELLYFIN_API_KEY","usernameEnv":"","passwordEnv":""},
"plex":{"enabled":false,"url":"","mediaTypes":[],"tokenEnv":"REQUESTARR_PLEX_TOKEN","usernameEnv":"","passwordEnv":""},
"romm":{"enabled":false,"url":"","mediaTypes":[],"tokenEnv":"REQUESTARR_ROMM_TOKEN","usernameEnv":"","passwordEnv":""},
"navidrome":{"enabled":true,"url":"https://music.example.test","mediaTypes":["album"],"tokenEnv":"","usernameEnv":"REQUESTARR_NAVIDROME_USERNAME","passwordEnv":"REQUESTARR_NAVIDROME_PASSWORD"}}`

func loadWith(t *testing.T, libraries string, runtime Runtime) Draft {
	t.Helper()
	var l LibrariesSettings
	if err := json.Unmarshal([]byte(libraries), &l); err != nil {
		t.Fatalf("a saved draft must still decode: %v", err)
	}
	svc := NewService(nil, fakeChecker{}, runtime)
	initial := svc.initial()
	initial.Settings.Libraries = l
	svc.repo = &memoryDrafts{draft: initial}
	draft, err := svc.Load(context.Background(), Actor{ID: "admin-1", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestLegacyDraftLoadsAndTakesTheEnvironmentUsername(t *testing.T) {
	draft := loadWith(t, legacyNavidromeLibraries, Runtime{LibraryUsernames: map[string]string{"navidrome": "svc-music"}})
	if got := draft.Settings.Libraries.Navidrome.Username; got != "svc-music" {
		t.Fatalf("username %q, want the environment-configured one", got)
	}
	encoded, _ := json.Marshal(draft)
	if strings.Contains(string(encoded), "usernameEnv") {
		t.Fatalf("retired usernameEnv still emitted: %s", encoded)
	}
	if err := draft.Settings.Validate(false); err != nil {
		t.Fatalf("normalized legacy draft must validate: %v", err)
	}
}

func TestSavedUsernameWinsOverTheEnvironmentOne(t *testing.T) {
	libraries := strings.Replace(legacyNavidromeLibraries, `"usernameEnv":"REQUESTARR_NAVIDROME_USERNAME"`, `"username":"drafted"`, 1)
	draft := loadWith(t, libraries, Runtime{LibraryUsernames: map[string]string{"navidrome": "svc-music"}})
	if got := draft.Settings.Libraries.Navidrome.Username; got != "drafted" {
		t.Fatalf("username %q", got)
	}
}

func TestNavidromeUsernameIsAPlainValidatedDraftField(t *testing.T) {
	settings := NewService(&memoryDrafts{}, fakeChecker{}, Runtime{}).initial().Settings
	settings.Libraries.Navidrome.Username = "requestarr"
	if err := settings.Validate(false); err != nil {
		t.Fatalf("plain username rejected: %v", err)
	}
	for name, bad := range map[string]string{"newline": "a\nb", "too long": strings.Repeat("u", 257)} {
		s := settings
		s.Libraries.Navidrome.Username = bad
		if err := s.Validate(false); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted", name)
		}
	}
	other := settings
	other.Libraries.Plex.Username = "x"
	if err := other.Validate(false); !errors.Is(err, ErrInvalid) {
		t.Error("a username on a non-Navidrome Source was accepted")
	}
}

func TestNavidromeEnvironmentListsThePlainUsernameAndTheSecretPassword(t *testing.T) {
	svc := NewService(&memoryDrafts{}, fakeChecker{}, Runtime{LibraryUsernames: map[string]string{"navidrome": "old"}, SecretsConfigured: map[secrets.Name]bool{secrets.NavidromePassword: true}})
	actor := Actor{ID: "admin-1", Role: "admin"}
	draft, _ := svc.Load(context.Background(), actor)
	s := draft.Settings
	s.Libraries.Navidrome = SourceSettings{Enabled: true, URL: "https://music.example.test", MediaTypes: []domain.MediaType{domain.MediaAlbum}, Username: "new", PasswordEnv: NavidromePasswordEnv}
	draft, err := svc.Save(context.Background(), actor, SaveInput{ExpectedRevision: draft.Revision, CurrentStep: StepLibraries, Settings: s})
	if err != nil {
		t.Fatal(err)
	}
	var user, pass *EnvironmentItem
	for i, item := range draft.Environment {
		switch item.Name {
		case NavidromeUsernameEnv:
			user = &draft.Environment[i]
		case NavidromePasswordEnv:
			pass = &draft.Environment[i]
		}
	}
	if user == nil || user.Secret || user.DesiredValue != "new" || !user.NeedsRestart || !user.Configured {
		t.Fatalf("username item %+v", user)
	}
	if pass == nil || !pass.Secret || pass.DesiredValue != "" || !pass.Configured {
		t.Fatalf("password item %+v", pass)
	}
}
