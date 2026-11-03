package checks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

func TestTMDBAuthenticationRejectionIsNotANetworkFailure(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-test-token" {
					t.Error("expected the raw token to be sent as a Bearer credential")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("untrusted-upstream-body private-test-token"))
			}))
			defer server.Close()
			logs := &devlogs.Recorder{}
			checker := checks.New(checks.Config{Diagnostics: logs, Secrets: secrets.Static{secrets.TMDBReadToken: "private-test-token"},
				Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}})
			code, err := checker.Check(context.Background(), setup.TargetTMDB, setup.Settings{Providers: setup.ProviderSettings{TMDBEnabled: true, TMDBTokenEnv: setup.TMDBTokenEnv}})
			want := setup.CheckCode("authentication_failed")
			if status == http.StatusInternalServerError {
				want = setup.CheckUnavailable
			}
			if code != want || want != setup.CheckUnavailable && err != nil {
				t.Fatalf("code=%q error=%v; want %q", code, err, want)
			}
			entries := logs.Snapshot().Entries
			if len(entries) != 2 || entries[0].HTTPStatus != status || string(entries[1].Code) != string(want) {
				t.Fatalf("unexpected safe diagnostic entries: %+v", entries)
			}
			data, _ := json.Marshal(entries)
			if strings.Contains(string(data), "private-test-token") || strings.Contains(string(data), "untrusted-upstream-body") {
				t.Fatal("diagnostics leaked credential or upstream response")
			}
		})
	}
}
