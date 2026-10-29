package checks_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

func TestCheckDiagnosticsClassifyWithoutSecrets(t *testing.T) {
	const canary = "hostile-secret-canary"
	for _, tc := range []struct {
		name              string
		status            int
		contentType, body string
		err               error
		want              devlogs.Code
	}{
		{"http", 401, "text/html", canary, nil, devlogs.CodeHTTPStatus},
		{"parse", 200, "application/json", canary, nil, devlogs.CodeMalformed},
		{"type", 200, "text/html", canary, nil, devlogs.CodeContentType},
		{"size", 200, "application/json", strings.Repeat("x", (1<<20)+1), nil, devlogs.CodeResponseSize},
		{"network", 0, "", "", errors.New(canary), devlogs.CodeNetwork},
		{"timeout", 0, "", "", context.DeadlineExceeded, devlogs.CodeTimeout},
		{"dns", 0, "", "", &net.DNSError{Err: canary, Name: canary}, devlogs.CodeDNS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &devlogs.Recorder{}
			cfg, settings := readyConfig(), readySettings()
			cfg.Diagnostics = logs
			cfg.Secrets = secrets.Static{secrets.TMDBReadToken: canary}
			cfg.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.contentType}, "X-Secret": {canary}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			code, err := checks.New(cfg).Check(context.Background(), setup.TargetTMDB, settings)
			wantCheck := setup.CheckUnavailable
			if tc.status == http.StatusUnauthorized {
				wantCheck = setup.CheckAuthentication
			}
			if code != wantCheck || wantCheck == setup.CheckAuthentication && err != nil || wantCheck == setup.CheckUnavailable && (err == nil || err.Error() != "setup connection check failed") {
				t.Fatalf("result = %s, %v", code, err)
			}
			s := logs.Snapshot()
			if len(s.Entries) != 2 || s.Entries[0].Code != tc.want || s.Entries[0].HTTPStatus != tc.status || s.Entries[1].Stage != devlogs.StageCheck || s.Entries[1].Code != devlogs.Code(wantCheck) {
				t.Fatalf("diagnostics = %+v", s)
			}
			data, _ := json.Marshal(s)
			if strings.Contains(string(data), canary) {
				t.Fatalf("secret leaked: %s", data)
			}
		})
	}
}

func TestLibraryDiagnosticsDoNotRetainURLUsernameOrBody(t *testing.T) {
	logs := &devlogs.Recorder{}
	cfg := checks.Config{Diagnostics: logs, Secrets: secrets.Static{secrets.NavidromePassword: "password-canary"}, Runtime: setup.Runtime{Development: true}}
	cfg.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("body-canary"))}, nil
	})}
	s := setup.Settings{Libraries: setup.LibrariesSettings{Navidrome: setup.SourceSettings{Enabled: true, URL: "http://url-canary.test", Username: "username-canary", PasswordEnv: setup.NavidromePasswordEnv, MediaTypes: []domain.MediaType{domain.MediaAlbum}}}}
	_, _ = checks.New(cfg).Check(context.Background(), setup.TargetNavidrome, s)
	got := logs.Snapshot()
	if len(got.Entries) != 2 || got.Entries[0].Code != devlogs.CodeMalformed {
		t.Fatalf("diagnostics = %+v", got)
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "canary") {
		t.Fatalf("secret leaked: %s", data)
	}
}

func TestLibraryDiagnosticContentTypes(t *testing.T) {
	const canary = "hostile-secret-canary"
	for _, library := range []struct {
		target setup.Target
		body   string
	}{
		{setup.TargetJellyfin, `{"ServerName":"test"}`},
		{setup.TargetPlex, `<MediaContainer size="1"/>`},
		{setup.TargetRomm, `[]`},
		{setup.TargetNavidrome, `{"subsonic-response":{"status":"ok"}}`},
	} {
		for _, contentType := range []string{"application/json; charset=utf-8", "application/xml; charset=utf-8", "text/xml", "text/html; private=" + canary, "", "invalid " + canary} {
			for _, status := range []int{200, 401, 403} {
				t.Run(string(library.target)+"/"+contentType+"/"+http.StatusText(status), func(t *testing.T) {
					logs := &devlogs.Recorder{}
					cfg := checks.Config{Diagnostics: logs, Runtime: setup.Runtime{Development: true}, Secrets: secrets.Static{secrets.JellyfinAPIKey: canary, secrets.PlexToken: canary, secrets.RommToken: canary, secrets.NavidromePassword: canary}}
					body := library.body
					if strings.HasPrefix(contentType, "text/html") {
						body = "<html>Login " + canary + "</html>"
					}
					cfg.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
					})}
					source := setup.SourceSettings{Enabled: true, URL: "http://library.test", MediaTypes: []domain.MediaType{domain.MediaMovie}}
					libraries := setup.LibrariesSettings{}
					switch library.target {
					case setup.TargetJellyfin:
						source.TokenEnv = setup.JellyfinAPIKeyEnv
						libraries.Jellyfin = source
					case setup.TargetPlex:
						source.TokenEnv = setup.PlexTokenEnv
						libraries.Plex = source
					case setup.TargetRomm:
						source.TokenEnv = setup.RommTokenEnv
						libraries.Romm = source
					case setup.TargetNavidrome:
						source.Username = "music-user"
						source.PasswordEnv = setup.NavidromePasswordEnv
						source.MediaTypes = []domain.MediaType{domain.MediaAlbum}
						libraries.Navidrome = source
					}
					want := devlogs.CodeContentType
					accepted := strings.HasPrefix(contentType, "application/json")
					if library.target == setup.TargetPlex {
						accepted = strings.HasPrefix(contentType, "application/xml") || contentType == "text/xml"
					}
					if status != 200 {
						want = devlogs.CodeHTTPStatus
					} else if accepted {
						want = devlogs.CodeConnected
					}
					code, err := checks.New(cfg).Check(context.Background(), library.target, setup.Settings{Libraries: libraries})
					if want == devlogs.CodeConnected {
						if code != setup.CheckOK || err != nil {
							t.Fatalf("result = %s, %v", code, err)
						}
					} else if code != setup.CheckUnavailable || err == nil || err.Error() != "setup connection check failed" {
						t.Fatalf("result = %s, %v", code, err)
					}
					snapshot := logs.Snapshot()
					if len(snapshot.Entries) != 2 || snapshot.Entries[0].Code != want || snapshot.Entries[0].HTTPStatus != status {
						t.Fatalf("diagnostics = %+v, want %s", snapshot, want)
					}
					data, _ := json.Marshal(snapshot)
					if strings.Contains(string(data), canary) {
						t.Fatalf("secret leaked: %s", data)
					}
				})
			}
		}
	}
}

func TestIGDBDiagnosticStages(t *testing.T) {
	for _, failStage := range []int{0, 1, 2} {
		t.Run(string(rune('0'+failStage)), func(t *testing.T) {
			logs := &devlogs.Recorder{}
			cfg := readyConfig()
			cfg.Diagnostics = logs
			calls := 0
			cfg.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				body := string(fixture(t, "oauth.json"))
				if calls == 2 {
					body = string(fixture(t, "platforms.json"))
				}
				status := 200
				if calls == failStage {
					status = 403
					body = "secret-canary"
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			_, _ = checks.New(cfg).Check(context.Background(), setup.TargetIGDB, readySettings())
			entries := logs.Snapshot().Entries
			wantLen := 3
			if failStage == 1 {
				wantLen = 2
			}
			if len(entries) != wantLen || entries[0].Stage != devlogs.StageOAuth || (wantLen == 3 && entries[1].Stage != devlogs.StagePlatforms) {
				t.Fatalf("stages = %+v", entries)
			}
			if failStage != 0 && entries[failStage-1].Code != devlogs.CodeHTTPStatus {
				t.Fatalf("failure = %+v", entries)
			}
		})
	}
}

func TestDiagnosticConfigurationOutcomes(t *testing.T) {
	logs := &devlogs.Recorder{}
	cfg, settings := readyConfig(), readySettings()
	cfg.Diagnostics = logs
	settings.Providers.TMDBEnabled = false
	_, _ = checks.New(cfg).Check(context.Background(), setup.TargetTMDB, settings)
	settings.Providers.TMDBEnabled = true
	cfg.Secrets = secrets.Static{}
	_, _ = checks.New(cfg).Check(context.Background(), setup.TargetTMDB, settings)
	settings.General.InstanceName = ""
	_, _ = checks.New(cfg).Check(context.Background(), setup.TargetGeneral, settings)
	e := logs.Snapshot().Entries
	if len(e) != 3 || e[0].Code != devlogs.CodeDisabled || e[0].Outcome != "skipped" || e[1].Code != devlogs.CodeEnvironment || e[2].Code != devlogs.CodeIncomplete {
		t.Fatalf("configuration = %+v", e)
	}
}

func TestLibraryAndNotificationDiagnosticHTTPOutcomes(t *testing.T) {
	for _, target := range []setup.Target{setup.TargetJellyfin, setup.TargetNotifications} {
		for _, status := range []int{200, 403} {
			logs := &devlogs.Recorder{}
			cfg := checks.Config{Diagnostics: logs, Secrets: secrets.Static{secrets.JellyfinAPIKey: "secret-canary", secrets.AppriseConfigKey: "key-canary"}, Runtime: setup.Runtime{Development: true, AppriseURL: "http://url-canary.test"}}
			cfg.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"value":"body-canary"}`))}, nil
			})}
			settings := setup.Settings{
				Libraries:     setup.LibrariesSettings{Jellyfin: setup.SourceSettings{Enabled: true, URL: "http://url-canary.test", TokenEnv: setup.JellyfinAPIKeyEnv, MediaTypes: []domain.MediaType{domain.MediaMovie}}},
				Notifications: setup.NotificationSettings{AppriseEnabled: true, AppriseURL: cfg.Runtime.AppriseURL, AppriseConfigKeyEnv: setup.AppriseConfigKeyEnv},
			}
			_, _ = checks.New(cfg).Check(context.Background(), target, settings)
			e := logs.Snapshot().Entries
			want := devlogs.CodeConnected
			if status == 403 {
				want = devlogs.CodeHTTPStatus
			}
			if len(e) != 2 || e[0].Target != target || e[0].HTTPStatus != status || e[0].Code != want {
				t.Fatalf("target=%s status=%d: %+v", target, status, e)
			}
			data, _ := json.Marshal(e)
			if strings.Contains(string(data), "canary") {
				t.Fatalf("secret leaked: %s", data)
			}
		}
	}
}
