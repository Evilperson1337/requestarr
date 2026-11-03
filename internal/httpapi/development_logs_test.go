package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/setup"
)

type developmentLogsReadiness struct {
	opStatusReadiness
	logs *devlogs.Recorder
}

func (s *developmentLogsReadiness) DevelopmentLogs() *devlogs.Recorder { return s.logs }

func getDevelopmentAPI(ready ReadinessChecker, role, path string) *httptest.ResponseRecorder {
	h := New(testLogger(), buildinfo.Info{}, ready, &auth.Handler{Repo: statusAuthRepo{user: auth.User{ID: "u", Role: role}}, Secret: "test-secret"})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/"+path, nil)
	if role != "" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDevelopmentLogsRoleMatrixAndCapability(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		ready := &developmentLogsReadiness{}
		if enabled {
			ready.logs = &devlogs.Recorder{}
			for i := 0; i < 205; i++ {
				ready.logs.Record(setup.TargetTMDB, devlogs.StageCheck, devlogs.CodeConnected, 0, 0)
			}
		}
		for _, role := range []string{"", "viewer", "requester", "admin"} {
			w := getDevelopmentAPI(ready, role, "development-logs")
			want := http.StatusOK
			switch {
			case role == "":
				want = http.StatusUnauthorized
			case !enabled:
				want = http.StatusNotFound
			case role != "admin":
				want = http.StatusForbidden
			}
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("enabled=%v role=%q: status=%d cache=%q body=%s", enabled, role, w.Code, w.Header().Get("Cache-Control"), w.Body.String())
			}
			if want == http.StatusOK {
				var got devlogs.Snapshot
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Capacity != 200 || len(got.Entries) != 200 || got.Entries[0].Seq != 6 {
					t.Fatalf("snapshot=%+v err=%v", got, err)
				}
			}
			status := getDevelopmentAPI(ready, role, "status")
			var body map[string]json.RawMessage
			if err := json.Unmarshal(status.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			capability, present := body["developmentLogsEnabled"]
			if present != (enabled && role == "admin") || (present && string(capability) != "true") {
				t.Fatalf("capability role=%q enabled=%v: %s", role, enabled, status.Body.String())
			}
		}
	}
}

func TestDevelopmentLogsAbsentWithoutOptionalInterface(t *testing.T) {
	w := getDevelopmentAPI(&opStatusReadiness{}, "admin", "development-logs")
	assertErrorEnvelope(t, w, http.StatusNotFound, "not_found")
}

func TestDevelopmentLogsEmptySnapshot(t *testing.T) {
	w := getDevelopmentAPI(&developmentLogsReadiness{logs: &devlogs.Recorder{}}, "admin", "development-logs")
	if w.Code != http.StatusOK || w.Body.String() != "{\"entries\":[],\"capacity\":200}\n" {
		t.Fatalf("empty snapshot: %d %s", w.Code, w.Body.String())
	}
}
