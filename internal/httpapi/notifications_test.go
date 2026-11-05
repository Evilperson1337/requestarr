package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/notifications"
)

type unreadBackend struct {
	fakeReadiness
	unread map[string]int
	asked  []string
}

func (b *unreadBackend) ListInbox(context.Context, string) ([]notifications.InboxItem, error) {
	return nil, nil
}
func (b *unreadBackend) MarkInboxRead(_ context.Context, userID, _ string) (bool, error) {
	b.unread[userID]--
	return true, nil
}
func (b *unreadBackend) CountUnreadInbox(_ context.Context, userID string) (int, error) {
	b.asked = append(b.asked, userID)
	return b.unread[userID], nil
}

func unreadCall(t *testing.T, b *unreadBackend, userID, role, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := New(testLogger(), buildinfo.Info{}, b, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: userID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(""))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUnreadSummaryIsScopedToTheSignedInPerson(t *testing.T) {
	b := &unreadBackend{unread: map[string]int{"viewer-1": 2, "admin-1": 5}}
	for _, c := range []struct{ id, role, want string }{{"viewer-1", "viewer", `"unread":2`}, {"admin-1", "admin", `"unread":5`}, {"nobody", "viewer", `"unread":0`}} {
		w := unreadCall(t, b, c.id, c.role, http.MethodGet, "/notifications/summary")
		if w.Code != 200 || !strings.Contains(w.Body.String(), c.want) {
			t.Fatalf("%s: %d %s, want %s", c.id, w.Code, w.Body.String(), c.want)
		}
		if got := b.asked[len(b.asked)-1]; got != c.id {
			t.Fatalf("counted for %q, want %q", got, c.id)
		}
	}
	if w := unreadCall(t, b, "viewer-1", "viewer", http.MethodPost, "/notifications/"+matrixItemID+"/read"); w.Code != 204 {
		t.Fatalf("mark read: %d", w.Code)
	}
	if w := unreadCall(t, b, "viewer-1", "viewer", http.MethodGet, "/notifications/summary"); !strings.Contains(w.Body.String(), `"unread":1`) {
		t.Fatalf("after mark read: %s", w.Body.String())
	}
}
