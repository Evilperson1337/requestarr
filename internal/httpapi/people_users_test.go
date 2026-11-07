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

// The list tells the page which roles the signed-in role may give each person,
// so the page offers only those and never compares role names.
func TestUsersListOffersOnlyTheRoleChoicesTheSignedInRoleMayMake(t *testing.T) {
	ready := userListReadiness{users: []postgres.AdminUser{
		{ID: matrixUserID, DisplayName: "Administrator", Role: "superuser", Builtin: true},
		{ID: "admin-1", DisplayName: "Me", Role: "admin"},
		{ID: "u-admin", DisplayName: "Other admin", Role: "admin"},
		{ID: "u-owner", DisplayName: "Owner", Role: "superuser"},
		{ID: "u-viewer", DisplayName: "Vic", Role: "viewer"},
	}}
	choices := func(role string) map[string]string {
		w := peopleCall(role, ready, "GET", "/admin/users", "")
		if w.Code != 200 {
			t.Fatalf("%s: status %d", role, w.Code)
		}
		var body struct {
			Users []struct {
				ID          string   `json:"id"`
				RoleChoices []string `json:"roleChoices"`
			} `json:"users"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, u := range body.Users {
			if u.RoleChoices == nil {
				t.Fatalf("%s: roleChoices missing for %s (an array, empty when none)", role, u.ID)
			}
			out[u.ID] = strings.Join(u.RoleChoices, ",")
		}
		return out
	}
	// The signed-in person is always "admin-1", so nobody is offered a change to their own role.
	for role, want := range map[string]map[string]string{
		"admin":     {matrixUserID: "", "admin-1": "", "u-admin": "", "u-owner": "", "u-viewer": "viewer,requester"},
		"superuser": {matrixUserID: "", "admin-1": "", "u-admin": "viewer,requester,admin,superuser", "u-owner": "viewer,requester,admin,superuser", "u-viewer": "viewer,requester,admin,superuser"},
	} {
		got := choices(role)
		for id, w := range want {
			if got[id] != w {
				t.Errorf("%s sees choices %q for %s, want %q", role, got[id], id, w)
			}
		}
	}
}

// The list says when a person's role is set by their groups, and whether group
// mapping is on, so the page can warn that a manual change is overridden at
// the person's next sign-in. It carries no group names and no group lists.
func TestUsersListSaysWhenRolesComeFromGroups(t *testing.T) {
	ready := userListReadiness{users: []postgres.AdminUser{
		{ID: "u-group", DisplayName: "Grace", Role: "admin", RoleFromGroups: true},
		{ID: "u-manual", DisplayName: "Manny", Role: "viewer"},
	}}
	w := peopleCall("superuser", ready, "GET", "/admin/users", "")
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var body struct {
		RolesFromGroups bool `json:"rolesFromGroups"`
		Users           []struct {
			ID             string `json:"id"`
			RoleFromGroups bool   `json:"roleFromGroups"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.RolesFromGroups || len(body.Users) != 2 || !body.Users[0].RoleFromGroups || body.Users[1].RoleFromGroups {
		t.Fatalf("body = %s", w.Body.String())
	}
}
