package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
)

type fixturePreviewReader struct {
	state domain.MoviePreview
	err   error
	calls int
}

func (f *fixturePreviewReader) MoviePreview(context.Context, string, string) (domain.MoviePreview, error) {
	f.calls++
	return f.state, f.err
}

func TestMoviePreviewReadsLocalStateWithRoleAppropriateActions(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		state      domain.MoviePreview
		want       string
	}{
		{"new", "requester", domain.MoviePreview{Availability: domain.AvailabilityUnknown}, `"canRequest":true`},
		{"viewer", "viewer", domain.MoviePreview{Availability: domain.AvailabilityUnknown}, `"canRequest":false`},
		{"available", "admin", domain.MoviePreview{Availability: domain.AvailabilityAvailable}, `"canRequest":false`},
		{"existing", "viewer", domain.MoviePreview{Availability: domain.AvailabilityUnknown, Request: &domain.PreviewRequest{ID: matrixItemID, Status: domain.RequestRequested}}, `"canSubscribe":true`},
		{"closed", "requester", domain.MoviePreview{Availability: domain.AvailabilityUnknown, Request: &domain.PreviewRequest{ID: matrixItemID, Status: domain.RequestCancelled}}, `"canSubscribe":false`},
		{"subscribed", "requester", domain.MoviePreview{Availability: domain.AvailabilityUnknown, Request: &domain.PreviewRequest{ID: matrixItemID, Status: domain.RequestRequested, Subscribed: true}}, `"canSubscribe":false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fixturePreviewReader{state: tc.state}
			d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Repo: statusAuthRepo{user: auth.User{ID: matrixUserID, Role: tc.role}}, Secret: "test-secret"})
			d.MoviePreview = reader
			h, err := NewRouter(d)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/"+matrixItemID+"/preview", nil)
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != 200 || reader.calls != 1 || !strings.Contains(rec.Body.String(), tc.want) || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected local preview: status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMoviePreviewRejectsAnonymousAndInvalidIDsAndSanitizesFailure(t *testing.T) {
	reader := &fixturePreviewReader{err: errors.New("private-upstream-canary")}
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Repo: statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "admin"}}, Secret: "test-secret"})
	d.MoviePreview = reader
	h, err := NewRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path    string
		session bool
		want    int
	}{{matrixItemID, false, 401}, {"invalid", true, 400}, {matrixItemID, true, 503}} {
		r := httptest.NewRequest("GET", "/api/v1/catalog/"+tc.path+"/preview", nil)
		if tc.session {
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != tc.want || strings.Contains(rec.Body.String(), "private-upstream-canary") {
			t.Fatalf("status=%d want=%d", rec.Code, tc.want)
		}
	}
	if reader.calls != 1 {
		t.Fatal("invalid or anonymous input reached preview storage")
	}
}
