package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sessionRepoFake serves one fixed session and records claim attempts, so the
// claim and /auth/me behaviour can be tested through the handler alone.
type sessionRepoFake struct {
	authRepoFake
	user                      User
	complete                  bool
	setupErr                  error
	claimErr                  error
	claims                    int
	claimIssuer, claimSubject string
}

func (f *sessionRepoFake) GetSession(_ context.Context, hash []byte, _ time.Time) (User, []byte, time.Time, error) {
	if string(hash) != string(Hash("secret", "session-token")) {
		return User{}, nil, time.Time{}, errors.New("missing")
	}
	return f.user, Hash("secret", "csrf-token"), time.Now().Add(time.Hour), nil
}
func (f *sessionRepoFake) ClaimAdmin(_ context.Context, _, issuer, subject string, _ []byte, _ time.Time) error {
	f.claims++
	f.claimIssuer, f.claimSubject = issuer, subject
	return f.claimErr
}
func (f *sessionRepoFake) SetupComplete(context.Context) (bool, error) { return f.complete, f.setupErr }

const goodToken = "0123456789abcdef0123456789abcdef0123456789a"

func newSessionHandler(repo *sessionRepoFake) *Handler {
	return &Handler{Repo: repo, Setup: repo, Secret: "secret", Issuer: "https://id.example.test", PublicURL: "https://app.example"}
}

func signedIn(r *http.Request) *http.Request {
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session-token"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	return r
}

func claim(h *Handler, body string, session bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/setup/claim-admin", strings.NewReader(body))
	if session {
		r = signedIn(r)
	}
	rec := httptest.NewRecorder()
	h.claimAdmin(rec, r)
	return rec
}

func me(h *Handler, session bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	if session {
		r = signedIn(r)
	}
	rec := httptest.NewRecorder()
	h.me(rec, r)
	return rec
}

func TestClaimTakesTheTokenOnlyAndUsesTheSessionIdentity(t *testing.T) {
	repo := &sessionRepoFake{user: User{ID: "u1", Role: "viewer", Issuer: "https://id.example.test", Subject: "sub-1"}}
	rec := claim(newSessionHandler(repo), `{"token":"`+goodToken+`"}`, true)
	if rec.Code != http.StatusNoContent || repo.claims != 1 || repo.claimIssuer != "https://id.example.test" || repo.claimSubject != "sub-1" {
		t.Fatalf("status=%d claims=%d identity=%q/%q", rec.Code, repo.claims, repo.claimIssuer, repo.claimSubject)
	}
}

func TestClaimAcceptsASentIdentityOnlyWhenItEqualsTheSession(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"same":              {`{"issuer":"https://id.example.test","subject":"sub-1","token":"` + goodToken + `"}`, http.StatusNoContent},
		"other subject":     {`{"issuer":"https://id.example.test","subject":"sub-2","token":"` + goodToken + `"}`, http.StatusForbidden},
		"other issuer":      {`{"issuer":"https://evil.example.test","subject":"sub-1","token":"` + goodToken + `"}`, http.StatusForbidden},
		"subject only":      {`{"subject":"sub-2","token":"` + goodToken + `"}`, http.StatusForbidden},
		"issuer only":       {`{"issuer":"https://evil.example.test","token":"` + goodToken + `"}`, http.StatusForbidden},
		"no token":          {`{}`, http.StatusForbidden},
		"short token":       {`{"token":"short"}`, http.StatusForbidden},
		"unknown field":     {`{"token":"` + goodToken + `","userId":"u2"}`, http.StatusForbidden},
		"trailing document": {`{"token":"` + goodToken + `"}{}`, http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &sessionRepoFake{user: User{ID: "u1", Role: "viewer", Issuer: "https://id.example.test", Subject: "sub-1"}}
			rec := claim(newSessionHandler(repo), tc.body, true)
			if rec.Code != tc.want {
				t.Fatalf("status=%d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusForbidden && repo.claims != 0 {
				t.Fatal("a rejected request reached the claim store")
			}
		})
	}
}

func TestClaimWithoutASessionOrWithAStoreRefusalIsGenericAndLeaksNothing(t *testing.T) {
	repo := &sessionRepoFake{user: User{ID: "u1", Issuer: "https://id.example.test", Subject: "sub-1"}, claimErr: errors.New("token expired for sub-1")}
	h := newSessionHandler(repo)
	if rec := claim(h, `{"token":"`+goodToken+`"}`, false); rec.Code != http.StatusForbidden || repo.claims != 0 {
		t.Fatalf("no session: status=%d claims=%d", rec.Code, repo.claims)
	}
	rec := claim(h, `{"token":"`+goodToken+`"}`, true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}
	for _, leak := range []string{"sub-1", "id.example.test", "expired", goodToken} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("claim failure leaks %q: %s", leak, rec.Body.String())
		}
	}
}

func TestMeShowsTheCallersOwnIdentityOnlyWhileSetupIsUnclaimed(t *testing.T) {
	repo := &sessionRepoFake{user: User{ID: "u1", Role: "viewer", Issuer: "https://id.example.test", Subject: "sub-1"}}
	var body struct {
		FirstAdminClaim *struct {
			Issuer             string `json:"issuer"`
			Subject            string `json:"subject"`
			IssuerIsConfigured bool   `json:"issuerIsConfigured"`
		} `json:"firstAdminClaim"`
	}
	rec := me(newSessionHandler(repo), true)
	c := body.FirstAdminClaim
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.FirstAdminClaim == nil {
		t.Fatalf("unclaimed: %v %s", err, rec.Body.String())
	}
	c = body.FirstAdminClaim
	if c.Subject != "sub-1" || c.Issuer != "https://id.example.test" || !c.IssuerIsConfigured {
		t.Fatalf("claim=%+v", *c)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("identity response must be no-store")
	}
}

func TestMeExposesNoIdentityOnceSetupIsCompleteOrUnknownOrUnauthenticated(t *testing.T) {
	user := User{ID: "u1", Role: "viewer", Issuer: "https://id.example.test", Subject: "sub-1"}
	for name, repo := range map[string]*sessionRepoFake{
		"complete viewer": {user: user, complete: true},
		"complete admin":  {user: User{ID: "u2", Role: "admin", Issuer: "https://id.example.test", Subject: "sub-9"}, complete: true},
		"lookup failed":   {user: user, setupErr: errors.New("db down")},
	} {
		t.Run(name, func(t *testing.T) {
			rec := me(newSessionHandler(repo), true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d", rec.Code)
			}
			for _, leak := range []string{"firstAdminClaim", "sub-1", "sub-9", "id.example.test"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("response exposes %q: %s", leak, rec.Body.String())
				}
			}
		})
	}
	rec := me(newSessionHandler(&sessionRepoFake{user: user}), false)
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "sub-1") {
		t.Fatalf("anonymous: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMeFlagsAnIssuerThatDiffersFromTheConfiguredOne(t *testing.T) {
	repo := &sessionRepoFake{user: User{ID: "u1", Issuer: "https://other.example.test", Subject: "sub-1"}}
	rec := me(newSessionHandler(repo), true)
	if !strings.Contains(rec.Body.String(), `"issuerIsConfigured":false`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
