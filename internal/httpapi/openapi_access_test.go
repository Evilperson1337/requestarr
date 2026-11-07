package httpapi

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

// The contract names the access level of every superuser-only operation with
// x-required-role, and only of those.
func TestOpenAPINamesTheSuperuserOnlyOperations(t *testing.T) {
	file, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	path, op := "", ""
	pathLine := regexp.MustCompile(`^  (/\S*):\s*$`)
	methodLine := regexp.MustCompile(`^    (get|put|post|delete|patch):\s*$`)
	inPaths := false
	for _, line := range strings.Split(string(file), "\n") {
		if line != "" && line[0] != ' ' && line[0] != '#' {
			inPaths = strings.HasPrefix(line, "paths:")
			continue
		}
		if !inPaths {
			continue
		}
		if m := pathLine.FindStringSubmatch(line); m != nil {
			path = m[1]
		} else if m := methodLine.FindStringSubmatch(line); m != nil {
			op = strings.ToUpper(m[1]) + " " + path
		} else if strings.TrimSpace(line) == "x-required-role: superuser" {
			documented[op] = true
		}
	}
	_, rt := newRouter(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	for _, p := range rt.Policies() {
		if p.Access == accessSuperuser && !documented[p.Pattern] {
			t.Errorf("%s is superuser-only but the contract lacks x-required-role: superuser", p.Pattern)
		}
		delete(documented, p.Pattern)
	}
	for op := range documented {
		t.Errorf("%s is marked superuser-only in the contract but the route table says otherwise", op)
	}
}
