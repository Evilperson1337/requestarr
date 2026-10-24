package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
)

func TestHandlerServesBuiltIndexAndReferencedAssets(t *testing.T) {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		t.Fatalf("embedded dist: %v", err)
	}
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		// dist is built by `make web-build`, not committed. CI always builds it, so
		// a missing build there is a failure rather than a skip.
		if os.Getenv("CI") != "" {
			t.Fatalf("embedded UI not built before tests: %v", err)
		}
		t.Skipf("embedded UI not built (run make web-build): %v", err)
	}
	asset := regexp.MustCompile(`(?:src|href)="(/assets/[^\"]+)"`).FindSubmatch(index)
	if len(asset) < 2 {
		t.Fatalf("index.html has no built /assets reference: %s", index)
	}
	for _, prefix := range [][]byte{[]byte("/src/"), []byte(".tsx")} {
		if stringContains(index, prefix) {
			t.Fatalf("index.html contains source reference %q", prefix)
		}
	}
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK || !regexp.MustCompile(`(?:src|href)="/assets/`).Match(rr.Body.Bytes()) {
		t.Fatalf("GET / status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, string(asset[1]), nil))
	if rr.Code != http.StatusOK || rr.Body.Len() == 0 {
		t.Fatalf("GET %s status=%d body=%q", asset[1], rr.Code, rr.Body.String())
	}
}

func stringContains(value, fragment []byte) bool {
	return len(fragment) == 0 || (len(value) >= len(fragment) && func() bool {
		for i := 0; i+len(fragment) <= len(value); i++ {
			if string(value[i:i+len(fragment)]) == string(fragment) {
				return true
			}
		}
		return false
	}())
}
