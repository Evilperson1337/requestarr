package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

type fakeLibrary struct {
	fakeReadiness
	err error
}

func (f fakeLibrary) RecentlyAdded(_ context.Context, limit int, _ []domain.MediaType) ([]domain.RecentLibraryItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []domain.RecentLibraryItem{{ID: "i1", Title: "Arrival", Year: 2016, MediaType: domain.MediaMovie, Source: "Plex", AddedAt: time.Unix(1700000000, 0).UTC(), ArtworkURL: "/api/v1/library/items/i1/artwork"}}, nil
}
func (f fakeLibrary) Artwork(_ context.Context, id string) (inventory.Artwork, error) {
	switch id {
	case "none":
		return inventory.Artwork{}, ErrNoArtwork
	case "broken":
		return inventory.Artwork{}, errors.New("GET http://192.168.1.250:32400/photo?X-Plex-Token=secret failed")
	}
	return inventory.Artwork{ContentType: "image/jpeg", Data: []byte("jpeg")}, nil
}

func libraryRequest(role, path string, lib fakeLibrary) *httptest.ResponseRecorder {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	h := NewWithSetup(logger, buildinfo.Info{}, lib, nil, nil, nil,
		&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRecentlyAddedIsOpenToEveryRoleAndCachedBriefly(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		w := libraryRequest(role, "/library/recent", fakeLibrary{})
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"Arrival"`) || !strings.Contains(w.Header().Get("Cache-Control"), "private") {
			t.Fatalf("%s: %d %s %v", role, w.Code, w.Body.String(), w.Header())
		}
	}
	if w := libraryRequest("admin", "/library/recent", fakeLibrary{err: errors.New("db: password=hunter2")}); w.Code != 500 || strings.Contains(w.Body.String(), "hunter2") {
		t.Fatalf("failure must be generic: %d %s", w.Code, w.Body.String())
	}
}

func TestArtworkIsServedAsAnImageAndNeverEchoesLibraryErrors(t *testing.T) {
	w := libraryRequest("viewer", "/library/items/i1/artwork", fakeLibrary{})
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Body.String() != "jpeg" {
		t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body.String())
	}
	if w := libraryRequest("viewer", "/library/items/none/artwork", fakeLibrary{}); w.Code != 404 {
		t.Fatalf("no artwork = %d", w.Code)
	}
	w = libraryRequest("viewer", "/library/items/broken/artwork", fakeLibrary{})
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "192.168") {
		t.Fatalf("library error leaked: %d %s", w.Code, w.Body.String())
	}
}

func (f fakeLibrary) Browse(_ context.Context, q domain.LibraryQuery) (domain.LibraryPage, error) {
	if q.After == "bad" {
		return domain.LibraryPage{}, ErrBadLibraryCursor
	}
	next := "next"
	return domain.LibraryPage{Items: []domain.LibraryItem{{ID: "i1", Title: string(q.Type) + "|" + q.Search + "|" + q.Source, MediaType: q.Type}}, NextCursor: &next}, nil
}
func (f fakeLibrary) Item(_ context.Context, id string) (domain.LibraryItem, error) {
	if id == "missing" {
		return domain.LibraryItem{}, ErrLibraryItemNotFound
	}
	return domain.LibraryItem{ID: id, Title: "Arrival", OpenIssues: 2}, nil
}

func TestLibraryBrowseValidatesItsQueryAndPassesItThrough(t *testing.T) {
	w := libraryRequest("viewer", "/library/items?type=movie&q=arr&source=plex&limit=10", fakeLibrary{})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"movie|arr|plex"`) || !strings.Contains(w.Body.String(), `"nextCursor":"next"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for name, path := range map[string]string{
		"no type":         "/library/items",
		"a song":          "/library/items?type=song",
		"unknown source":  "/library/items?type=movie&source=emby",
		"limit too big":   "/library/items?type=movie&limit=101",
		"limit not a num": "/library/items?type=movie&limit=x",
		"huge search":     "/library/items?type=movie&q=" + strings.Repeat("a", 101),
	} {
		if w := libraryRequest("viewer", path, fakeLibrary{}); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := libraryRequest("viewer", "/library/items?type=movie&cursor=bad", fakeLibrary{}); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_cursor") {
		t.Fatalf("bad cursor: %d %s", w.Code, w.Body.String())
	}
}

func TestLibraryItemDetail(t *testing.T) {
	if w := libraryRequest("viewer", "/library/items/abc", fakeLibrary{}); w.Code != 200 || !strings.Contains(w.Body.String(), `"openIssues":2`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := libraryRequest("viewer", "/library/items/missing", fakeLibrary{}); w.Code != 404 {
		t.Fatalf("missing = %d", w.Code)
	}
}
