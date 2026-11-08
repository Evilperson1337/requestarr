package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// Only "Authorization: Bearer rqd_..." makes a request a token request. Any other
// Authorization header (a reverse proxy's Basic, another bearer value) is
// ignored, so the request is judged as a session request and never feeds the
// bearer throttle.
func TestOtherAuthorizationHeadersAreIgnoredOnSessionAndPublicRoutes(t *testing.T) {
	for _, header := range []string{"Basic dXNlcjpwdw==", "Bearer eyJhbGciOiJIUzI1NiJ9.e30.abc", "Bearer", "Digest x=1"} {
		f := newBearerFixture(t)
		for _, path := range []string{"/admin/users", "/admin/logs", "/version", "/auth/options"} {
			with := f.call("GET", path, header, "203.0.113.9")
			without := f.call("GET", path, "", "203.0.113.9")
			if with.Code != without.Code {
				t.Fatalf("%q on %s: %d, without the header %d", header, path, with.Code, without.Code)
			}
		}
		for i := 0; i < 20; i++ {
			f.call("GET", "/admin/logs", header, "203.0.113.9")
		}
		if w := f.call("GET", "/admin/logs", "Bearer "+f.token, "203.0.113.9"); w.Code != 200 {
			t.Fatalf("%q fed the throttle: %d", header, w.Code)
		}
		if strings.Contains(f.logs.String(), "diagnostics token refused") {
			t.Fatalf("%q was judged as a token request:\n%s", header, f.logs)
		}
	}
}

func TestAValidTokenOnAnUnmarkedRouteIsRefusedAndGarbageThereIsThrottled(t *testing.T) {
	f := newBearerFixture(t)
	for i := 0; i < 20; i++ {
		assertErrorEnvelope(t, f.call("GET", "/admin/users", "Bearer "+f.token, "203.0.113.10"), 401, "authentication_required")
	}
	if w := f.call("GET", "/admin/logs", "Bearer "+f.token, "203.0.113.10"); w.Code != 200 {
		t.Fatalf("a valid token on unmarked routes was counted: %d", w.Code)
	}
	for i := 0; i < 6; i++ {
		f.call("GET", "/admin/users", "Bearer rqd_garbage", "203.0.113.11")
	}
	if w := f.call("GET", "/admin/logs", "Bearer "+f.token, "203.0.113.11"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("garbage on an unmarked route did not throttle: %d", w.Code)
	}
}
