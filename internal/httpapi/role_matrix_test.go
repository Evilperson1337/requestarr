package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
)

const matrixUserID = "11111111-1111-4111-8111-111111111111"
const matrixItemID = "22222222-2222-4222-8222-222222222222"
const matrixToken = "matrix-bootstrap-token-12345678901234567890"

type mutationCase struct {
	name, pattern, path, body string
	policy                    string
	status                    int
	effect                    string
}

// This table is also the input to the production-route coverage guard. A
// valid positive request is required: denial must not be a validation error.
func mutationCases() []mutationCase {
	return []mutationCase{
		{"create request", "POST /requests", "/requests", `{"catalogId":"` + matrixItemID + `","platforms":{"any":false}}`, "requester", 201, "request.create"},
		{"resolve metadata", "POST /resolve", "/resolve", `{"url":"https://www.themoviedb.org/movie/11"}`, "authenticated", 200, "metadata.resolve"},
		{"subscribe", "PUT /requests/{requestID}/subscription", "/requests/" + matrixItemID + "/subscription", "", "authenticated", 200, "request.subscribe"},
		{"leave", "DELETE /requests/{requestID}/subscription", "/requests/" + matrixItemID + "/subscription", "", "authenticated", 200, "request.leave"},
		{"approve", "POST /requests/{requestID}/transition", "/requests/" + matrixItemID + "/transition", transitionBody("approved"), "admin", 200, "request.approved"},
		{"deny", "POST /requests/{requestID}/transition", "/requests/" + matrixItemID + "/transition", transitionBody("denied"), "admin", 200, "request.denied"},
		{"cancel", "POST /requests/{requestID}/transition", "/requests/" + matrixItemID + "/transition", transitionBody("cancelled"), "admin", 200, "request.cancelled"},
		{"read notification", "POST /notifications/{notificationID}/read", "/notifications/" + matrixItemID + "/read", "", "authenticated", 204, "notification.read"},
		{"sync source", "POST /admin/sources/{source}/sync", "/admin/sources/jellyfin/sync", "", "admin", 202, "source.sync"},
		{"change role", "PUT /admin/users/{userID}/role", "/admin/users/" + matrixItemID + "/role", `{"role":"viewer"}`, "admin", 204, "user.role"},
		{"accept match", "POST /admin/match-suggestions/{suggestionID}/accept", "/admin/match-suggestions/" + matrixItemID + "/accept", "", "admin", 204, "match.accept"},
		{"dismiss match", "POST /admin/match-suggestions/{suggestionID}/dismiss", "/admin/match-suggestions/" + matrixItemID + "/dismiss", "", "admin", 204, "match.dismiss"},
		{"logout", "POST /auth/logout", "/auth/logout", "", "authenticated", 204, "session.revoke"},
		// Bootstrap authorization is the matching issuer/subject plus a one-time
		// token, not a pre-existing admin role. Its real repository is tested
		// independently for single-use and last-admin serialization.
		{"claim initial admin", "POST /setup/claim-admin", "/setup/claim-admin", `{"issuer":"https://issuer.example","subject":"subject-1","token":"` + matrixToken + `"}`, "authenticated", 204, "bootstrap.claim"},
		{"save setup draft", "PUT /admin/setup/draft", "/admin/setup/draft", validSetupDraftBody(), "admin", 200, "setup.save"},
		{"validate setup", "POST /admin/setup/validate", "/admin/setup/validate", `{"expectedRevision":1,"target":"general"}`, "admin", 200, "setup.validate"},
		{"finish setup", "POST /admin/setup/finish", "/admin/setup/finish", `{"expectedRevision":1}`, "admin", 200, "setup.finish"},
		{"set stored secret", "PUT /admin/secrets/{name}", "/admin/secrets/tmdb_read_token", `{"value":"matrix-secret-value"}`, "admin", 204, "secret.set"},
		{"clear stored secret", "DELETE /admin/secrets/{name}", "/admin/secrets/tmdb_read_token", "", "admin", 204, "secret.clear"},
		{"validate library", "POST /admin/setup/validate", "/admin/setup/validate", `{"expectedRevision":1,"target":"library:plex"}`, "admin", 200, "setup.validate"},
	}
}

func transitionBody(status string) string {
	return `{"toStatus":"` + status + `","expectedVersion":3,"operationId":"33333333-3333-4333-8333-333333333333"}`
}

type matrixBackend struct {
	fakeReadiness
	requests.Repository
	t       *testing.T
	effects []string
}

func (b *matrixBackend) record(effect, actor string) {
	b.t.Helper()
	if actor != matrixUserID {
		b.t.Errorf("%s actor=%q want current user", effect, actor)
	}
	b.effects = append(b.effects, effect)
}

func (*matrixBackend) Upsert(context.Context, domain.ResolvedItem) (domain.CatalogItem, error) {
	return domain.CatalogItem{}, nil
}

func (*matrixBackend) CatalogMediaType(context.Context, string) (domain.MediaType, error) {
	return domain.MediaMovie, nil
}

func (*matrixBackend) ValidatePlatforms(context.Context, string, domain.PlatformSelection) error {
	return nil
}

func (b *matrixBackend) CreateOrGet(_ context.Context, request domain.Request, subscription domain.RequestSubscription) (domain.Request, bool, error) {
	b.record("request.create", request.RequestedBy)
	if subscription.UserID != request.RequestedBy || subscription.Source != domain.SubscriptionRequester {
		b.t.Error("creation did not subscribe the current requester")
	}
	request.ID = matrixItemID
	return request, true, nil
}

func (b *matrixBackend) MergeSelection(context.Context, string, string, domain.Selection) (domain.Request, bool, error) {
	return domain.Request{}, false, nil
}

func (b *matrixBackend) ResolveAndUpsert(context.Context, string) (domain.ResolvedItem, error) {
	b.record("metadata.resolve", matrixUserID)
	return domain.ResolvedItem{Item: domain.CatalogItem{ID: matrixItemID, MediaType: domain.MediaMovie}}, nil
}

func (b *matrixBackend) Subscribe(_ context.Context, requestID, userID string) (bool, error) {
	b.record("request.subscribe", userID)
	return true, nil
}

func (b *matrixBackend) Unsubscribe(_ context.Context, requestID, userID string, _ bool) (bool, bool, error) {
	b.record("request.leave", userID)
	return true, false, nil
}

func (b *matrixBackend) Transition(_ context.Context, input requests.TransitionInput) (domain.Request, bool, error) {
	b.record("request."+string(input.ToStatus), input.Actor.ID)
	if input.Actor.Role != "admin" || input.ExpectedVersion != 3 || input.RequestID != matrixItemID {
		b.t.Errorf("unexpected transition: %+v", input)
	}
	return domain.Request{ID: input.RequestID, Status: input.ToStatus, Version: 4}, true, nil
}

func (*matrixBackend) ListInbox(context.Context, string) ([]notifications.InboxItem, error) {
	return nil, nil
}

func (b *matrixBackend) MarkInboxRead(_ context.Context, userID, notificationID string) (bool, error) {
	b.record("notification.read", userID)
	return true, nil
}

func (b *matrixBackend) SyncNow(_ context.Context, source string) (jobs.Run, error) {
	b.record("source.sync", matrixUserID)
	return jobs.Run{ID: matrixItemID, Status: jobs.StatusRunning, Trigger: jobs.TriggerManual}, nil
}

func (b *matrixBackend) ChangeUserRole(_ context.Context, actor, target, role string, _ time.Time) error {
	b.record("user.role", actor)
	if target != matrixItemID || role != "viewer" {
		b.t.Errorf("unexpected role change target=%q role=%q", target, role)
	}
	return nil
}

func (*matrixBackend) ListMatchSuggestions(context.Context) ([]domain.MatchSuggestion, error) {
	return nil, nil
}

func (b *matrixBackend) ReviewMatchSuggestion(_ context.Context, id, actor, decision string) error {
	b.record("match."+decision, actor)
	return nil
}

type matrixAuthRepo struct {
	csrfAuthRepo
	backend *matrixBackend
}

func (r matrixAuthRepo) RevokeSession(context.Context, []byte, time.Time) error {
	r.backend.record("session.revoke", r.user.ID)
	return nil
}

func (r matrixAuthRepo) ClaimAdmin(_ context.Context, userID, issuer, subject string, tokenHash []byte, _ time.Time) error {
	expected := sha256.Sum256([]byte(matrixToken))
	if userID != r.user.ID || issuer != r.user.Issuer || subject != r.user.Subject || !reflect.DeepEqual(tokenHash, expected[:]) {
		return fmt.Errorf("invalid bootstrap identity or token")
	}
	r.backend.record("bootstrap.claim", userID)
	return nil
}

func (b *matrixBackend) Load(context.Context, setup.Actor) (setup.Draft, error) {
	return setup.Draft{Revision: 1, CurrentStep: setup.StepGeneral, Inactive: true}, nil
}
func (b *matrixBackend) Save(_ context.Context, actor setup.Actor, _ setup.SaveInput) (setup.Draft, error) {
	b.record("setup.save", actor.ID)
	return setup.Draft{Revision: 2, Inactive: true}, nil
}
func (b *matrixBackend) Finish(_ context.Context, actor setup.Actor, _ int64) (setup.Draft, error) {
	b.record("setup.finish", actor.ID)
	return setup.Draft{Revision: 1, Inactive: true}, nil
}
func (b *matrixBackend) Validate(_ context.Context, actor setup.Actor, _ int64, _ setup.Target) (setup.Draft, error) {
	b.record("setup.validate", actor.ID)
	return setup.Draft{Revision: 1, Inactive: true}, nil
}

func (b *matrixBackend) Status(context.Context) ([]secrets.Status, bool, error) {
	return nil, true, nil
}
func (b *matrixBackend) Set(_ context.Context, actor string, _ secrets.Name, _ string) error {
	b.record("secret.set", actor)
	return nil
}
func (b *matrixBackend) Clear(_ context.Context, actor string, _ secrets.Name) error {
	b.record("secret.clear", actor)
	return nil
}

func matrixRequest(backend *matrixBackend, tc mutationCase, role, csrf, origin string) *httptest.ResponseRecorder {
	repo := matrixAuthRepo{csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role, Issuer: "https://issuer.example", Subject: "subject-1"}}}, backend}
	h := NewWithSetup(testLogger(), buildinfo.Info{}, backend, backend, backend, backend, &auth.Handler{Repo: repo, Secret: "test-secret", PublicURL: "http://example.test"})
	method, _, _ := strings.Cut(tc.pattern, " ")
	r := httptest.NewRequest(method, "/api/v1"+tc.path, strings.NewReader(tc.body))
	if role != "unauthenticated" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMutatingEndpointRoleMatrix(t *testing.T) {
	for _, endpoint := range mutationCases() {
		for _, role := range []string{"unauthenticated", "viewer", "requester", "admin"} {
			t.Run(endpoint.name+"/"+role, func(t *testing.T) {
				backend := &matrixBackend{t: t}
				w := matrixRequest(backend, endpoint, role, "csrf-token", "http://example.test")
				status, code := endpoint.status, ""
				if role == "unauthenticated" {
					status, code = 401, "authentication_required"
				} else if endpoint.policy == "admin" && role != "admin" || endpoint.policy == "requester" && role == "viewer" {
					status, code = 403, "forbidden"
				}
				var effects []string
				if code != "" {
					assertErrorEnvelope(t, w, status, code)
				} else {
					if w.Code != status {
						t.Fatalf("status=%d want %d body=%s", w.Code, status, w.Body.String())
					}
					effects = []string{endpoint.effect}
				}
				if !reflect.DeepEqual(backend.effects, effects) {
					t.Fatalf("mutation effects=%v want %v", backend.effects, effects)
				}
			})
		}
	}
}

func TestMutatingEndpointsRequireCSRFAndOriginAcrossAuthenticatedRoles(t *testing.T) {
	for _, endpoint := range mutationCases() {
		for _, role := range []string{"viewer", "requester", "admin"} {
			for _, protection := range []struct {
				name, csrf, origin, code string
			}{
				{"missing CSRF", "", "http://example.test", "csrf_invalid"},
				{"wrong CSRF", "not-the-session-token", "http://example.test", "csrf_invalid"},
				{"untrusted origin", "csrf-token", "http://untrusted.example", "origin_rejected"},
				{"missing origin", "csrf-token", "", "origin_rejected"},
			} {
				t.Run(endpoint.name+"/"+role+"/"+protection.name, func(t *testing.T) {
					backend := &matrixBackend{t: t}
					w := matrixRequest(backend, endpoint, role, protection.csrf, protection.origin)
					assertErrorEnvelope(t, w, 403, protection.code)
					if len(backend.effects) != 0 {
						t.Fatalf("invalid CSRF/origin reached mutations: %v", backend.effects)
					}
				})
			}
		}
	}
}

func TestBootstrapClaimRequiresIdentityAndTokenNotExistingAdminRole(t *testing.T) {
	var endpoint mutationCase
	for _, candidate := range mutationCases() {
		if candidate.pattern == "POST /setup/claim-admin" {
			endpoint = candidate
		}
	}
	if endpoint.pattern == "" {
		t.Fatal("bootstrap claim row missing")
	}
	for _, role := range []string{"viewer", "requester", "admin"} {
		for _, invalid := range []struct{ name, from, to string }{
			{"wrong subject", "subject-1", "somebody-else"},
			{"wrong issuer", "https://issuer.example", "https://other.example"},
			{"wrong token", matrixToken, "invalid-bootstrap-token-12345678901234567890"},
		} {
			t.Run(role+"/"+invalid.name, func(t *testing.T) {
				tc := endpoint
				tc.body = strings.Replace(tc.body, invalid.from, invalid.to, 1)
				backend := &matrixBackend{t: t}
				assertErrorEnvelope(t, matrixRequest(backend, tc, role, "csrf-token", "http://example.test"), 403, "claim_unavailable")
				if len(backend.effects) != 0 {
					t.Fatalf("invalid bootstrap identity/token reached claim: %v", backend.effects)
				}
			})
		}
	}
}
