package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// The session answer carries what the role may do, so the web app never
// compares role names.
func TestAuthMeReturnsTheRoleAndItsCapabilities(t *testing.T) {
	f := newFixture(t)
	f.store.creds[adminUserID].MustChange = false
	want := map[string]string{
		"viewer":    `{"request":false,"manageRequests":false,"manageSystem":false}`,
		"requester": `{"request":true,"manageRequests":false,"manageSystem":false}`,
		"admin":     `{"request":true,"manageRequests":true,"manageSystem":true}`,
	}
	n := 0
	for role, caps := range want {
		n++
		id := fmt.Sprintf("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb%02d", n)
		f.store.addUser(id, role)
		token := "tok-" + role
		if err := f.store.CreateSession(context.Background(), id, Hash(testSecret, token), Hash(testSecret, "csrf-"+role), f.clock.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		cs := []*http.Cookie{{Name: sessionCookie, Value: token}, {Name: "requestarr_csrf", Value: "csrf-" + role}}
		w := f.authed(http.MethodGet, "/auth/me", "", cs)
		var body struct {
			User         struct{ Role string }
			Capabilities json.RawMessage
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.User.Role != role || string(body.Capabilities) != caps {
			t.Errorf("%s: role=%q capabilities=%s, want %s", role, body.User.Role, body.Capabilities, caps)
		}
	}
}
