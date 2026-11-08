package checks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

// rommPlatforms is a /api/platforms answer shaped like RomM 5.4.1's
// PlatformSchema: every platform carries its firmware list, abbreviation and
// alternative names, so a library with many platforms is far past 64 KiB.
func rommPlatforms(n int) []byte {
	platforms := make([]map[string]any, n)
	for i := range platforms {
		firmware := make([]map[string]any, 6)
		for j := range firmware {
			firmware[j] = map[string]any{"id": i*10 + j, "file_name": fmt.Sprintf("bios_%d_%d.bin", i, j), "file_path": fmt.Sprintf("/romm/library/bios/platform-%d", i), "file_size_bytes": 524288,
				"crc_hash": "0d3c1a2b", "md5_hash": "6b2a1e9c3d4f5a6b7c8d9e0f1a2b3c4d", "sha1_hash": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "is_verified": true, "missing_from_fs": false}
		}
		platforms[i] = map[string]any{"id": i + 1, "slug": fmt.Sprintf("platform-%d", i), "fs_slug": fmt.Sprintf("platform-%d", i), "name": fmt.Sprintf("Platform %d", i), "custom_name": nil,
			"igdb_id": 100 + i, "rom_count": 250, "firmware": firmware, "firmware_count": len(firmware), "abbreviation": "P", "alternative_names": []string{"Alternative name one", "Alternative name two"},
			"display_name": fmt.Sprintf("Platform %d", i), "is_identified": true, "missing_from_fs": false, "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}
	}
	data, _ := json.Marshal(platforms)
	return data
}

func rommCheck(t *testing.T, body []byte) setup.CheckCode {
	return rommCheckLogged(t, body, nil)
}

func rommCheckLogged(t *testing.T, body []byte, logger *slog.Logger) setup.CheckCode {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	cfg := checks.Config{Runtime: setup.Runtime{Development: true}, Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}, Secrets: secrets.Static{secrets.RommToken: "rmm_token"}, Logger: logger}
	libraries := setup.LibrariesSettings{Romm: setup.SourceSettings{Enabled: true, URL: "https://romm.example", MediaTypes: []domain.MediaType{domain.MediaGame}, TokenEnv: setup.RommTokenEnv}}
	code, _ := checks.New(cfg).Check(context.Background(), setup.TargetRomm, setup.Settings{Libraries: libraries})
	return code
}

func TestRommCheckAcceptsALargePlatformList(t *testing.T) {
	body := rommPlatforms(80)
	if len(body) < 64<<10 {
		t.Fatalf("fixture is only %d bytes; it must be past the old 64 KiB limit", len(body))
	}
	if code := rommCheck(t, body); code != setup.CheckOK {
		t.Fatalf("a %d-byte platform list: Check=%s", len(body), code)
	}
}

func TestRommCheckWantsAPlatformList(t *testing.T) {
	if code := rommCheck(t, []byte(`{"detail":"Forbidden"}`)); code == setup.CheckOK {
		t.Fatal("an object is not a platform list")
	}
}

// A failed check is logged with its fixed category, so Settings, Logs says why
// without the token, the address or the answer.
func TestAFailedCheckIsLoggedWithItsCategoryOnly(t *testing.T) {
	var out bytes.Buffer
	body := []byte(`{"detail":"rmm_secret_echo"}`)
	if code := rommCheckLogged(t, body, slog.New(slog.NewTextHandler(&out, nil))); code == setup.CheckOK {
		t.Fatal("check passed")
	}
	log := out.String()
	if !strings.Contains(log, "connection check failed") || !strings.Contains(log, "target=library:romm") || !strings.Contains(log, "code=malformed_response") {
		t.Fatalf("log = %q", log)
	}
	for _, leak := range []string{"rmm_", "romm.example", "detail"} {
		if strings.Contains(log, leak) {
			t.Fatalf("log holds %q: %q", leak, log)
		}
	}
}
