package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
)

type fixedResolver struct {
	item domain.ResolvedItem
	err  error
}

func (f fixedResolver) ResolveAndUpsert(context.Context, string) (domain.ResolvedItem, error) {
	return f.item, f.err
}

func resolveRequest(t *testing.T, logger *slog.Logger, resolver Resolver) *httptest.ResponseRecorder {
	t.Helper()
	repo := csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "user-1", Role: "requester"}}}
	h := NewWithMetadata(logger, buildinfo.Info{}, fakeReadiness{}, resolver, &auth.Handler{Repo: repo, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/resolve", strings.NewReader(`{"url":"https://www.igdb.com/games/call-of-duty-modern-warfare-4"}`))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// A game resolves with its details and the same request/availability preview a
// movie gets, so the web app can show the same confirmation screen.
func TestResolvedGameCarriesDetailsAndPreviewState(t *testing.T) {
	score := 81.5
	game := domain.ResolvedItem{
		Item:      domain.CatalogItem{ID: matrixItemID, MediaType: domain.MediaGame, Provider: "igdb", Title: "Modern Warfare 4"},
		Platforms: []domain.Platform{{Key: "igdb:6", Name: "PC"}},
		Game:      &domain.GameDetails{Overview: "The next chapter.", Developers: []string{"Infinity Ward"}, Score: &score, RatingCount: 40},
	}
	rec := resolveRequest(t, testLogger(), fixedResolver{item: game})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"Game":{"overview":"The next chapter."`, `"developers":["Infinity Ward"]`, `"Preview":{"availability"`, `"canRequest":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("response lacks %s: %s", want, body)
		}
	}
}

// The response stays generic, but the operator can read why in the log.
func TestFailedResolutionIsLoggedWithItsCauseAndAnsweredGenerically(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	rec := resolveRequest(t, logger, fixedResolver{err: errors.New("IGDB token request returned 403 Forbidden")})
	assertErrorEnvelope(t, rec, http.StatusBadRequest, "metadata_resolution_failed")
	if strings.Contains(rec.Body.String(), "IGDB") {
		t.Fatalf("the provider error leaked into the response: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "metadata resolution failed") || !strings.Contains(logs.String(), "403 Forbidden") || !strings.Contains(logs.String(), "request_id=") {
		t.Fatalf("log = %s", logs.String())
	}
}
