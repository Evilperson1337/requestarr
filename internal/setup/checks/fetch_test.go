package checks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/setup"
)

// Response judging is one function, so it is tested once, without secrets or a
// real provider.
func TestFetchJudgesStatusTypeSizeAndBody(t *testing.T) {
	big := strings.Repeat("x", 100)
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
		spec        fetchSpec
		wantErr     error
		wantCode    devlogs.Code
	}{
		{"healthy json", 200, "application/json; charset=utf-8", `{"ok":true}`, fetchSpec{types: []string{"application/json"}}, nil, devlogs.CodeConnected},
		{"401 is an authentication failure", 401, "text/html", "no", fetchSpec{types: []string{"application/json"}}, errAuthentication, devlogs.CodeHTTPStatus},
		{"403 is an authentication failure", 403, "application/json", `{}`, fetchSpec{}, errAuthentication, devlogs.CodeHTTPStatus},
		{"500", 500, "application/json", `{}`, fetchSpec{}, errCheck, devlogs.CodeHTTPStatus},
		{"wrong type", 200, "text/html", "<p>", fetchSpec{types: []string{"application/json"}}, errCheck, devlogs.CodeContentType},
		{"xml type accepted", 200, "text/xml", "<a/>", fetchSpec{types: []string{"application/xml", "text/xml"}}, nil, devlogs.CodeConnected},
		{"any type accepted", 200, "text/plain", "ok", fetchSpec{}, nil, devlogs.CodeConnected},
		{"too large", 200, "application/json", big, fetchSpec{types: []string{"application/json"}, maxBytes: 10}, errCheck, devlogs.CodeResponseSize},
		{"empty body", 200, "application/json", "", fetchSpec{types: []string{"application/json"}}, errCheck, devlogs.CodeMalformed},
		{"body rejected by validator", 200, "application/json", `{"ok":false}`, fetchSpec{types: []string{"application/json"}, validate: func(d []byte) bool { return strings.Contains(string(d), "true") }}, errCheck, devlogs.CodeMalformed},
		{"discarded body is not judged", 200, "text/plain", "", fetchSpec{discard: true}, nil, devlogs.CodeConnected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			logs := &devlogs.Recorder{}
			c := New(Config{Diagnostics: logs, Client: server.Client()})
			spec := tc.spec
			spec.stage, spec.method, spec.endpoint, spec.trusted = devlogs.StageRequest, http.MethodGet, server.URL, true
			if spec.maxBytes == 0 {
				spec.maxBytes = 1 << 10
			}
			ctx := context.WithValue(context.Background(), diagnosticTargetKey{}, setup.TargetGeneral)
			err := c.fetch(ctx, spec)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			entries := logs.Snapshot().Entries
			if len(entries) != 1 || entries[0].Code != tc.wantCode {
				t.Fatalf("diagnostics = %+v, want one entry with code %s", entries, tc.wantCode)
			}
		})
	}
}
