package auth

import (
	"bytes"
	"context"
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
