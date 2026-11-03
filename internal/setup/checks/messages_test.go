package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type fixedSource struct{ resolved secrets.Resolved }

func (f fixedSource) Resolve(context.Context, secrets.Name) (secrets.Resolved, error) {
	return f.resolved, nil
}

func describing(source secrets.Source, value string) *Checker {
	return New(Config{Secrets: fixedSource{secrets.Resolved{Value: value, Source: source}}})
}

// Message mapping needs no HTTP and no real secrets: a result plus the source
// of the secret is enough.
func TestDescribeNamesTheSecretSourceWithoutTheValue(t *testing.T) {
	for _, tc := range []struct {
		target setup.Target
		source secrets.Source
		code   setup.CheckCode
		want   string
	}{
		{setup.TargetTMDB, secrets.SourceEnvironment, setup.CheckOK, "token from the environment"},
		{setup.TargetTMDB, secrets.SourceStored, setup.CheckOK, "stored token"},
		{setup.TargetTMDB, secrets.SourceShadowed, setup.CheckOK, "shadowed by environment"},
		{setup.TargetTMDB, secrets.SourceStored, setup.CheckUnavailable, "stored token"},
		{setup.TargetTMDB, secrets.SourceStored, setup.CheckAuthentication, "stored token"},
		{setup.TargetIGDB, secrets.SourceStored, setup.CheckOK, "stored client secret"},
		{setup.TargetIGDB, secrets.SourceEnvironment, setup.CheckOK, "client secret from the environment"},
		{setup.TargetNotifications, secrets.SourceStored, setup.CheckOK, "stored configuration key"},
		{setup.TargetNotifications, secrets.SourceShadowed, setup.CheckOK, "shadowed by environment"},
		{setup.TargetJellyfin, secrets.SourceStored, setup.CheckOK, "stored API key"},
		{setup.TargetPlex, secrets.SourceStored, setup.CheckOK, "stored token"},
		{setup.TargetRomm, secrets.SourceStored, setup.CheckOK, "stored token"},
		{setup.TargetNavidrome, secrets.SourceStored, setup.CheckOK, "stored password"},
	} {
		msg := describing(tc.source, "probe-value").Describe(context.Background(), tc.target, tc.code)
		if !strings.Contains(msg, tc.want) || strings.Contains(msg, "probe-value") {
			t.Errorf("%s/%s/%s: message %q", tc.target, tc.source, tc.code, msg)
		}
	}
}

// An unreadable stored secret is reported whatever the code, so a lost master
// key is visible even when the step is Incomplete or Disabled.
func TestDescribeReportsAnUnreadableSecretForEveryCode(t *testing.T) {
	for _, code := range []setup.CheckCode{setup.CheckOK, setup.CheckUnavailable, setup.CheckEnvironment, setup.CheckIncomplete, setup.CheckDisabled} {
		for _, target := range []setup.Target{setup.TargetTMDB, setup.TargetIGDB, setup.TargetNotifications, setup.TargetJellyfin, setup.TargetNavidrome} {
			if msg := describing(secrets.SourceUnreadable, "").Describe(context.Background(), target, code); !strings.Contains(msg, "cannot be read") {
				t.Errorf("%s/%s: %q does not report the unreadable secret", target, code, msg)
			}
		}
	}
}

func TestDescribeNeverClaimsAHealthySecretWasCheckedWhenNothingWasAttempted(t *testing.T) {
	for _, code := range []setup.CheckCode{setup.CheckIncomplete, setup.CheckDisabled, setup.CheckEnvironment} {
		if msg := describing(secrets.SourceStored, "v").Describe(context.Background(), setup.TargetTMDB, code); strings.Contains(msg, "stored") {
			t.Errorf("%s: %q claims a secret was checked", code, msg)
		}
	}
}

func TestDescribeOtherTargetsAreUnchangedBySecrets(t *testing.T) {
	if msg := describing(secrets.SourceStored, "v").Describe(context.Background(), setup.TargetGeneral, setup.CheckOK); msg != "Connection succeeded. Draft settings remain inactive." {
		t.Fatalf("message %q", msg)
	}
}

func TestDescribeWordingPerCode(t *testing.T) {
	c := New(Config{})
	ctx := context.Background()
	for _, want := range []string{"HTTP 401 or 403", "API Read Access Token", "not the v3 API key", "without a Bearer prefix"} {
		if msg := c.Describe(ctx, setup.TargetTMDB, setup.CheckAuthentication); !strings.Contains(msg, want) {
			t.Errorf("TMDB authentication message lacks %q: %q", want, msg)
		}
	}
	igdb := c.Describe(ctx, setup.TargetIGDB, setup.CheckEnvironment)
	if !strings.Contains(igdb, "REQUESTARR_IGDB_CLIENT_ID") || !strings.Contains(igdb, "override") || strings.Contains(igdb, "Checked the") {
		t.Fatalf("misleading IGDB configuration message: %q", igdb)
	}
	if generic := c.Describe(ctx, setup.TargetPlex, setup.CheckEnvironment); !strings.Contains(generic, "environment variables") {
		t.Fatalf("generic environment message: %q", generic)
	}
}

// Every registered Source kind gets a secret note, whatever its credentials.
func TestDescribeCoversEveryRegisteredSourceKind(t *testing.T) {
	for _, kind := range sources.All() {
		msg := describing(secrets.SourceStored, "v").Describe(context.Background(), setup.LibraryTarget(kind.Slug), setup.CheckOK)
		if !strings.Contains(msg, "Checked the stored ") {
			t.Errorf("%s: %q has no secret note", kind.Slug, msg)
		}
	}
}
