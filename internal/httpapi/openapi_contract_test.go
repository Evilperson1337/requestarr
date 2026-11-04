package httpapi

import (
	"bufio"
	"encoding/json"
	"github.com/evilperson1337/requestarr/internal/domain"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

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

// specProperties lists the property names of a components schema, read with the same
// line scanner as specOperations. nested descends through named properties first, so
// ("LibraryItem", "details") lists the properties of LibraryItem.details.
func specProperties(t *testing.T, schema string, nested ...string) []string {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	indent := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	// child returns the lines nested under the first line named key, searching from "from".
	child := func(block []string, key string) []string {
		for i, line := range block {
			if strings.TrimSpace(line) == key+":" || strings.HasPrefix(strings.TrimSpace(line), key+": ") {
				var out []string
				for _, next := range block[i+1:] {
					if strings.TrimSpace(next) != "" && indent(next) <= indent(line) {
						break
					}
					out = append(out, next)
				}
				return out
			}
		}
		t.Fatalf("api/openapi.yaml has no %q under schema %s", key, schema)
		return nil
	}
	start := -1
	for i, line := range lines {
		if line == "    "+schema+":" {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("api/openapi.yaml has no schema %s", schema)
	}
	block := child(lines[start:], schema)
	for _, name := range nested {
		block = child(child(block, "properties"), name)
	}
	props := child(block, "properties")
	var names []string
	for _, line := range props {
		if indent(line) == indent(props[0]) && strings.Contains(line, ":") {
			names = append(names, strings.TrimSpace(strings.SplitN(line, ":", 2)[0]))
		}
	}
	sort.Strings(names)
	return names
}

// jsonKeys lists the keys a fully populated value marshals to.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// A schema that lists a field the server never sends, or omits one it does send, is a lie.
// Fill every field of the Go value so omitempty cannot hide one.
func TestOpenAPILibrarySchemasMatchTheGoTypes(t *testing.T) {
	details := domain.LibraryDetails{Synopsis: "s", Rating: 1, ContentRating: "R", Genres: []string{"g"}, RuntimeMinutes: 1}
	item := domain.LibraryItem{ID: "i", Title: "t", Year: 1, MediaType: domain.MediaMovie, Source: "s", SourceKind: "plex", AddedAt: time.Now(), Detail: "d", ArtworkURL: "a", Link: "l",
		OpenIssues: 1, IssueKinds: []domain.IssueKindOption{{}}, Details: &details, Tracks: []domain.LibraryTrack{{}}}
	for name, c := range map[string]struct {
		got    any
		schema []string
	}{
		"LibraryItem":         {item, []string{"LibraryItem"}},
		"LibraryItem.details": {details, []string{"LibraryItem", "details"}},
	} {
		if want, got := specProperties(t, c.schema[0], c.schema[1:]...), jsonKeys(t, c.got); strings.Join(want, ",") != strings.Join(got, ",") {
			t.Errorf("%s: api/openapi.yaml lists %v but the server sends %v", name, want, got)
		}
	}
}
