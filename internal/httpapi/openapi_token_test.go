package httpapi

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

// The contract marks exactly the token-readable routes with x-token-readable
// and declares the bearer scheme they use.
func TestOpenAPIMarksTheTokenReadableOperations(t *testing.T) {
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
		} else if strings.TrimSpace(line) == "x-token-readable: true" {
			documented[op] = true
		}
	}
	_, rt := newRouter(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	for _, p := range rt.Policies() {
		if p.TokenReadable && !documented[p.Pattern] {
			t.Errorf("%s is token-readable but the contract lacks x-token-readable: true", p.Pattern)
		}
		delete(documented, p.Pattern)
	}
	for op := range documented {
		t.Errorf("%s is marked token-readable in the contract but the route table says otherwise", op)
	}
	if !strings.Contains(string(file), "diagnosticsBearer:") {
		t.Error("the contract lacks the diagnosticsBearer security scheme")
	}
}
