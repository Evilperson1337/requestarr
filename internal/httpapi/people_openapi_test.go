package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

// The Users list marks the local administrator; the schema and the Go type agree.
func TestOpenAPIAdminUserMatchesTheGoType(t *testing.T) {
	got := jsonKeys(t, postgres.AdminUser{ID: "i", Email: "e", DisplayName: "d", Role: "admin", Builtin: true, CreatedAt: time.Now()})
	if want := specProperties(t, "AdminUser"); strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("api/openapi.yaml lists %v but the server sends %v", want, got)
	}
}
