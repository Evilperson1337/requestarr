// Command romm-sync-probe runs Requestarr's own RomM connection check and a
// full read-only sync against a live RomM server, with a token read from a
// private file the caller owns, and prints a safe summary: the check's outcome,
// the step a sync failed on, counts and timings. It prints no token and no
// response content beyond counts. Setup is the same as scripts/agent/romm-probe.sh
// (the directory ~/.config/requestarr-probe, or REQUESTARR_PROBE_DIR).
//
//	go run ./scripts/agent/romm-sync-probe
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/romm"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

func main() {
	dir := os.Getenv("REQUESTARR_PROBE_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "requestarr-probe")
	}
	tokenPath := filepath.Join(dir, "romm.token")
	info, err := os.Stat(tokenPath)
	if err != nil {
		fail("cannot read %s: %v", tokenPath, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		fail("refusing: %s must not be readable by others (mode %o)", tokenPath, info.Mode().Perm())
	}
	rawURL, err := os.ReadFile(filepath.Join(dir, "romm.url"))
	if err != nil {
		fail("cannot read romm.url: %v", err)
	}
	rawToken, _ := os.ReadFile(tokenPath)
	url, token := strings.TrimRight(strings.TrimSpace(string(rawURL)), "/"), strings.TrimSpace(string(rawToken))
	redact := func(s string) string { return strings.ReplaceAll(s, token, "[token]") }

	fmt.Println("RomM sync probe against", url)
	fmt.Println()

	// 1. The connection check Settings, Integrations runs.
	cfg := checks.Config{Runtime: setup.Runtime{}, Secrets: secrets.Static{secrets.RommToken: token}}
	libraries := setup.LibrariesSettings{Romm: setup.SourceSettings{Enabled: true, URL: url, MediaTypes: []domain.MediaType{domain.MediaGame}, TokenEnv: setup.RommTokenEnv}}
	checkCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	code, err := checks.New(cfg).Check(checkCtx, setup.TargetRomm, setup.Settings{Libraries: libraries})
	cancel()
	fmt.Printf("1 connection check (Settings, Integrations, Test): %s", code)
	if err != nil {
		fmt.Printf("  (%s)", redact(err.Error()))
	}
	fmt.Print("\n\n")

	// 2. A full sync, as the scheduled job does it.
	client, err := romm.New(url, token)
	if err != nil {
		fail("romm client: %s", redact(err.Error()))
	}
	ctx, steps := inventory.WithStepTracker(context.Background())
	ctx, stop := context.WithTimeout(ctx, 15*time.Minute)
	defer stop()
	started := time.Now()
	snap, err := client.Snapshot(ctx)
	took := time.Since(started).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("2 sync: FAILED after %s\n  step:   %s\n  reason: %s\n", took, steps.Current(), redact(err.Error()))
		os.Exit(1)
	}
	byPlatform := map[string]int{}
	withIGDB, withCover := 0, 0
	for _, item := range snap.Items {
		byPlatform[item.Detail]++
		if item.ExternalIDs["igdb"] != "" {
			withIGDB++
		}
		if item.ArtworkRef != "" {
			withCover++
		}
	}
	fmt.Printf("2 sync: OK in %s, complete=%v\n  games:               %d\n  with an IGDB id:     %d\n  with a cover:        %d\n  platforms with games: %d\n", took, snap.Complete, len(snap.Items), withIGDB, withCover, len(byPlatform))
	type row struct {
		name string
		n    int
	}
	var rows []row
	for name, n := range byPlatform {
		rows = append(rows, row{name, n})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
	for i, r := range rows {
		if i == 5 {
			break
		}
		fmt.Printf("    %-28s %d\n", r.name, r.n)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
