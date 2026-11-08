package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/domain"
)

// groupStore is a person who signs in with a role, plus the group-role path.
type groupStore struct {
	authRepoFake
	role       string // the role Login returns
	applied    []domain.Role
	applyErr   error
	sessionFor string
}

func (s *groupStore) Login(_ context.Context, _, _, _, _, uid string, _ time.Time) (User, error) {
	return User{ID: uid, Role: s.role}, nil
}
func (s *groupStore) CreateSession(_ context.Context, user string, _, _ []byte, _ time.Time) error {
	s.sessionFor = user
	return nil
}
func (s *groupStore) ApplyGroupRole(_ context.Context, _ string, role domain.Role, _ time.Time) error {
	s.applied = append(s.applied, role)
	return s.applyErr
}

type groupsProvider struct {
	authProviderFake
	groups []string
}

func (p groupsProvider) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return &oidc.Identity{Subject: "sub-1", Email: "a@example.test", DisplayName: "A", Groups: p.groups}, nil
}

// signIn drives the real login and callback handlers and returns the status.
func signIn(t *testing.T, store *groupStore, mapping domain.GroupMapping, groups []string, logs *bytes.Buffer) int {
	t.Helper()
	snap := &authsettings.Snapshot{Provider: groupsProvider{groups: groups}, Issuer: "https://id.example", Groups: mapping}
	h := &Handler{Repo: store, GroupRoles: store, OIDC: func() *authsettings.Snapshot { return snap }, Secret: "secret", PublicURL: "https://app.example"}
	if logs != nil {
		h.Logger = slog.New(slog.NewTextHandler(logs, nil))
	}
	loginRec := httptest.NewRecorder()
	h.login(loginRec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	cookies := loginRec.Result().Cookies()
	state := ""
	for _, c := range cookies {
		if c.Name == "requestarr_oidc_state" {
			state = c.Value
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state="+state, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.callback(rec, req)
	return rec.Code
}

func mappingOn() domain.GroupMapping {
	m := domain.DefaultGroupMapping()
	m.Enabled = true
	return m
}

func TestSignInAppliesTheRoleTheGroupsMapTo(t *testing.T) {
	cases := []struct {
		name    string
		have    string
		mapping domain.GroupMapping
		groups  []string
		want    []domain.Role
	}{
		{"raises", "viewer", mappingOn(), []string{"admin"}, []domain.Role{domain.RoleAdmin}},
		{"lowers", "superuser", mappingOn(), []string{"viewer"}, []domain.Role{domain.RoleViewer}},
		{"the most powerful group wins", "viewer", mappingOn(), []string{"viewer", "owner", "user"}, []domain.Role{domain.RoleSuperuser}},
		{"nothing to do when the role already matches", "requester", mappingOn(), []string{"user"}, nil},
		{"no matching group keeps the role", "admin", mappingOn(), []string{"staff"}, nil},
		{"no groups keeps the role", "requester", mappingOn(), nil, nil},
		{"switched off ignores the groups", "viewer", domain.DefaultGroupMapping(), []string{"owner"}, nil},
		{"case and a leading slash are ignored", "viewer", mappingOn(), []string{"/OWNER"}, []domain.Role{domain.RoleSuperuser}},
		{"custom names", "viewer", domain.GroupMapping{Enabled: true, Claim: "groups", Superuser: "Household", Admin: "a", Requester: "r", Viewer: "v"}, []string{"household"}, []domain.Role{domain.RoleSuperuser}},
	}
	for _, c := range cases {
		store := &groupStore{role: c.have}
		if code := signIn(t, store, c.mapping, c.groups, nil); code != http.StatusSeeOther {
			t.Fatalf("%s: status %d", c.name, code)
		}
		if len(store.applied) != len(c.want) || (len(c.want) == 1 && store.applied[0] != c.want[0]) {
			t.Errorf("%s: applied %v, want %v", c.name, store.applied, c.want)
		}
		if store.sessionFor == "" {
			t.Errorf("%s: no session was issued", c.name)
		}
	}
}

func TestAMappingThatWouldLeaveNoSuperuserIsSkippedAndSignInStillWorks(t *testing.T) {
	store := &groupStore{role: "superuser", applyErr: ErrLastSuperuser}
	var logs bytes.Buffer
	if code := signIn(t, store, mappingOn(), []string{"canary-group", "viewer"}, &logs); code != http.StatusSeeOther {
		t.Fatalf("sign-in must still work, got %d", code)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "no superuser") {
		t.Fatalf("expected a warning saying why, got %q", out)
	}
	if strings.Contains(out, "canary-group") || strings.Contains(out, "viewer") {
		t.Fatalf("the person's groups reached the log: %q", out)
	}
}

func TestAFailedGroupRoleChangeStopsTheSignIn(t *testing.T) {
	store := &groupStore{role: "viewer", applyErr: context.DeadlineExceeded}
	if code := signIn(t, store, mappingOn(), []string{"admin"}, nil); code != http.StatusInternalServerError {
		t.Fatalf("a role we could not apply must not sign the person in with a stale role, got %d", code)
	}
	if store.sessionFor != "" {
		t.Fatal("no session may be issued")
	}
}

// Every OIDC sign-in says in the log what the group mapping decided, without
// the group names, so an operator can see why a role did or did not change.
func TestSignInLogsWhatTheGroupMappingDecided(t *testing.T) {
	cases := []struct {
		name    string
		mapping domain.GroupMapping
		role    string
		groups  []string
		want    string
	}{
		{"mapping off", domain.DefaultGroupMapping(), "viewer", []string{"canary-group"}, "level=INFO msg=\"OIDC sign-in: roles from groups are off, role unchanged\""},
		{"no groups claim", mappingOn(), "viewer", nil, "level=WARN msg=\"OIDC sign-in: the ID token has no groups claim"},
		{"no match", mappingOn(), "viewer", []string{"canary-group", "canary-other"}, "no group matched a mapped group, role unchanged\" user_id="},
		{"already that role", mappingOn(), "admin", []string{"admin"}, "groups map to the role the person already has"},
		{"applied", mappingOn(), "viewer", []string{"owner"}, "msg=\"OIDC sign-in: role set from groups\""},
	}
	for _, c := range cases {
		var logs bytes.Buffer
		if code := signIn(t, &groupStore{role: c.role}, c.mapping, c.groups, &logs); code != http.StatusSeeOther {
			t.Fatalf("%s: sign-in got %d", c.name, code)
		}
		out := logs.String()
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: log %q lacks %q", c.name, out, c.want)
		}
		if strings.Contains(out, "canary") {
			t.Errorf("%s: a group name reached the log: %q", c.name, out)
		}
	}
	var logs bytes.Buffer
	signIn(t, &groupStore{role: "viewer"}, mappingOn(), []string{"canary-group", "canary-other"}, &logs)
	if !strings.Contains(logs.String(), "group_count=2") {
		t.Errorf("no-match log should count the groups: %q", logs.String())
	}
}

// A failed sign-in logs its step and reason, never the code, state or any
// text from the provider.
type failingProvider struct {
	authProviderFake
	err error
}

func (p failingProvider) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return nil, p.err
}

func TestFailedSignInsAreLoggedByStepAndReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"wrong client secret", &oidc.SignInError{Step: oidc.StepCodeExchange, Reason: "oauth_invalid_client"}, "step=code_exchange reason=oauth_invalid_client"},
		{"clock or issuer", &oidc.SignInError{Step: oidc.StepTokenVerify, Reason: "expired_check_server_clock"}, "step=id_token_verify reason=expired_check_server_clock"},
		{"anything else", errors.New("canary-provider-text"), "step=exchange reason=unknown"},
	}
	for _, c := range cases {
		snap := &authsettings.Snapshot{Provider: failingProvider{err: c.err}, Issuer: "https://id.example", Groups: mappingOn()}
		var logs bytes.Buffer
		store := &groupStore{role: "viewer"}
		h := &Handler{Repo: store, GroupRoles: store, OIDC: func() *authsettings.Snapshot { return snap }, Secret: "secret", PublicURL: "https://app.example", Logger: slog.New(slog.NewTextHandler(&logs, nil))}
		loginRec := httptest.NewRecorder()
		h.login(loginRec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=canary-code&state=", nil)
		state := ""
		for _, ck := range loginRec.Result().Cookies() {
			req.AddCookie(ck)
			if ck.Name == "requestarr_oidc_state" {
				state = ck.Value
			}
		}
		req.URL.RawQuery = "code=canary-code&state=" + state
		rec := httptest.NewRecorder()
		h.callback(rec, req)
		out := logs.String()
		if rec.Code != http.StatusUnauthorized || !strings.Contains(out, "OIDC sign-in failed") || !strings.Contains(out, c.want) {
			t.Errorf("%s: status %d, log %q", c.name, rec.Code, out)
		}
		if strings.Contains(out, "canary") {
			t.Errorf("%s: secret text reached the log: %q", c.name, out)
		}
	}
	// A request with no cookies says why it was refused.
	var logs bytes.Buffer
	h := &Handler{Repo: &groupStore{}, Secret: "secret", Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	rec := httptest.NewRecorder()
	h.callback(rec, httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state=s", nil))
	if !strings.Contains(logs.String(), "step=state_cookie reason=missing_or_mismatch") {
		t.Errorf("log = %q", logs.String())
	}
}
