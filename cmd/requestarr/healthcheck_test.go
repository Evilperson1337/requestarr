package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain lets tests run the real entry point: when the marker variable is
// set, the test binary behaves as the requestarr binary.
func TestMain(m *testing.M) {
	if os.Getenv("REQUESTARR_TEST_RUN_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// runBinary runs main() in a child process with args and extra env, returning
// the exit code and combined output.
func runBinary(t *testing.T, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "REQUESTARR_TEST_RUN_MAIN=1"}, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err == nil {
		return 0, out.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String()
	}
	t.Fatalf("run: %v", err)
	return -1, ""
}

func serverAddr(srv *httptest.Server) string { return strings.TrimPrefix(srv.URL, "http://") }

func TestHealthcheckReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	code, out := runBinary(t, []string{"REQUESTARR_HTTP_ADDR=" + serverAddr(srv)}, "healthcheck")
	if code != 0 || out != "" {
		t.Fatalf("exit = %d, output %q; want silent success", code, out)
	}
}

func TestHealthcheckNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "database password=hunter2 down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	code, out := runBinary(t, []string{"REQUESTARR_HTTP_ADDR=" + serverAddr(srv)}, "healthcheck")
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if strings.Contains(out, "hunter2") {
		t.Fatalf("response body leaked: %q", out)
	}
}

func TestHealthcheckNothingListening(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := serverAddr(srv)
	srv.Close()
	code, _ := runBinary(t, []string{"REQUESTARR_HTTP_ADDR=" + addr}, "healthcheck")
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
}

func TestHealthcheckHungServerTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	start := time.Now()
	code, _ := runBinary(t, []string{"REQUESTARR_HTTP_ADDR=" + serverAddr(srv)}, "healthcheck", "--timeout", "300ms")
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v, want bounded by timeout", time.Since(start))
	}
}

func TestHealthcheckWildcardAddrProbesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	for _, host := range []string{"", "0.0.0.0"} {
		code, out := runBinary(t, []string{"REQUESTARR_HTTP_ADDR=" + host + ":" + port}, "healthcheck")
		if code != 0 {
			t.Fatalf("addr %q: exit = %d, output %q", host, code, out)
		}
	}
}
