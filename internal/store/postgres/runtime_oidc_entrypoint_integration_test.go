//go:build integration

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The production entrypoint, not a fixture router: a fresh database and no
// OIDC variables must reach both the local sign-in and minimal public options.
func TestServerStartsWithoutOIDCEntrypointIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	binary := filepath.Join(t.TempDir(), "requestarr")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../../cmd/requestarr").CombinedOutput(); err != nil {
		t.Fatalf("build failed (%d bytes)", len(out))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("allocate server port failed")
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	origin := "http://" + addr
	cmd := exec.CommandContext(ctx, binary)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "REQUESTARR_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	const initial = "initial runtime admin password"
	cmd.Env = append(cmd.Env, "REQUESTARR_DATABASE_URL="+s.pool.Config().ConnString(), "REQUESTARR_ENV=development", "REQUESTARR_HTTP_ADDR="+addr, "REQUESTARR_PUBLIC_URL="+origin, "REQUESTARR_SESSION_HMAC_SECRET="+strings.Repeat("s", 32), "REQUESTARR_ADMIN_PASSWORD="+initial)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("server process failed to start")
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
		if strings.Contains(output.String(), initial) {
			t.Error("environment initial credential leaked to process output")
		}
	})
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := client.Get(origin + "/readyz")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no-OIDC server never became ready")
		}
		time.Sleep(25 * time.Millisecond)
	}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", origin+"/api/v1/auth/options", nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal("options unavailable")
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	res.Body.Close()
	var options map[string]any
	if res.StatusCode != 200 || json.Unmarshal(raw, &options) != nil || len(options) != 2 || options["oidcEnabled"] != false {
		t.Fatal("fresh server options incorrect")
	}
	req, _ = http.NewRequestWithContext(ctx, "POST", origin+"/api/v1/auth/local/login", strings.NewReader(`{"username":"admin","password":"`+initial+`"}`))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal("local sign-in unavailable")
	}
	defer res.Body.Close()
	var signedIn struct {
		MustChange bool `json:"mustChangePassword"`
	}
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&signedIn) != nil || !signedIn.MustChange {
		t.Fatal("fresh server local sign-in failed")
	}
}
