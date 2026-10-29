package httpapi

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/devlogs"
)

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// specOperations reads "METHOD /path" for every operation under the top-level
// paths key of api/openapi.yaml, with path parameters normalized to {}. It is a
// line scanner for the file's fixed two-space layout, so a path that drifts out
// of paths (for example under components) is not counted.
func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	file, err := os.Open("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ops := map[string]bool{}
	section, path := "", ""
	pathLine := regexp.MustCompile(`^  (/\S*):\s*$`)
	methodLine := regexp.MustCompile(`^    (get|put|post|delete|patch):\s*$`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && line[0] != ' ' && line[0] != '#' {
			section, path = strings.TrimSuffix(strings.TrimSpace(line), ":"), ""
			continue
		}
		if section != "paths" {
			continue
		}
		if m := pathLine.FindStringSubmatch(line); m != nil {
			path = pathParam.ReplaceAllString(m[1], "{}")
		} else if m := methodLine.FindStringSubmatch(line); m != nil && path != "" {
			ops[strings.ToUpper(m[1])+" "+path] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return ops
}

// api/openapi.yaml is the HTTP contract: every registered API route is
// documented there and every documented operation is served.
func TestOpenAPIMatchesTheRouteTable(t *testing.T) {
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, &fakeSetup{}, &auth.Handler{Secret: "test-secret"})
	d.DevelopmentLogs = &devlogs.Recorder{} // registered only when enabled
	_, rt, err := buildRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	for _, p := range rt.Policies() {
		routes[pathParam.ReplaceAllString(p.Pattern, "{}")] = true
	}
	spec := specOperations(t)
	var missing, extra []string
	for op := range routes {
		if !spec[op] {
			missing = append(missing, op)
		}
	}
	for op := range spec {
		if !routes[op] {
			extra = append(extra, op)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	for _, op := range missing {
		t.Errorf("route %s is not documented in api/openapi.yaml paths", op)
	}
	for _, op := range extra {
		t.Errorf("api/openapi.yaml documents %s, which no route serves", op)
	}
	if len(spec) < 20 {
		t.Fatalf("only %d operations found under paths; is api/openapi.yaml intact?", len(spec))
	}
}
