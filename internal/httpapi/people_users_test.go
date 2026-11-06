package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

// The Users list marks the local administrator (D040) and the server, not the page,
// keeps its role locked.

type userListReadiness struct {
	fakeReadiness
	users []postgres.AdminUser
	role  error
}

func (p userListReadiness) ListAdminUsers(context.Context) ([]postgres.AdminUser, error) {
	return p.users, nil
}
func (p userListReadiness) ChangeUserRole(context.Context, string, string, string, time.Time) error {
	return p.role
}

// peopleCall sends a request as the given role; an empty role is a caller with no session.
func peopleCall(role string, ready userListReadiness, method, path, body string) *httptest.ResponseRecorder {
	h := New(testLogger(), buildinfo.Info{}, ready, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "admin-1", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	if role != "" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUsersListMarksTheLocalAdministratorForAdminsOnly(t *testing.T) {
	ready := userListReadiness{users: []postgres.AdminUser{
		{ID: matrixUserID, DisplayName: "Administrator", Role: "admin", Builtin: true},
		{ID: matrixItemID, Email: "ann@example.test", DisplayName: "Ann", Role: "viewer"},
	}}
	for _, tc := range []struct {
		role string
		want int
	}{{"", 401}, {"viewer", 403}, {"requester", 403}, {"admin", 200}} {
		t.Run("role "+tc.role, func(t *testing.T) {
			w := peopleCall(tc.role, ready, "GET", "/admin/users", "")
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
			if tc.want != 200 {
				return
			}
			var body struct {
				Users []map[string]any `json:"users"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Users) != 2 {
				t.Fatalf("body not decodable: %v", err)
			}
			if body.Users[0]["builtin"] != true || body.Users[1]["builtin"] != false {
				t.Fatalf("builtin flags: %v %v", body.Users[0]["builtin"], body.Users[1]["builtin"])
			}
		})
	}
}

func TestRoleOfTheLocalAdministratorIsRefusedWithAClearCode(t *testing.T) {
	ready := userListReadiness{role: auth.ErrBuiltinRole}
	path := "/admin/users/" + matrixItemID + "/role"
	assertErrorEnvelope(t, peopleCall("admin", ready, "PUT", path, `{"role":"viewer"}`), 409, "builtin_admin_role")
	assertErrorEnvelope(t, peopleCall("viewer", ready, "PUT", path, `{"role":"viewer"}`), 403, "forbidden")
	assertErrorEnvelope(t, peopleCall("requester", ready, "PUT", path, `{"role":"viewer"}`), 403, "forbidden")
	assertErrorEnvelope(t, peopleCall("", ready, "PUT", path, `{"role":"viewer"}`), 401, "authentication_required")
}
