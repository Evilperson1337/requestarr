//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/issues"
)

// issueSource is a Source of its own, so these tests never change what another
// test's Source "latest snapshot" says.
var issueSourceID = newUUID()

func issueSource() inventory.Descriptor {
	return inventory.Descriptor{ID: issueSourceID, Kind: "navidrome", Name: "Issue fixture library", MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum}}
}

// libraryItem stores one Plex item and returns its inventory id.
func libraryItem(t *testing.T, ctx context.Context, s *Store, sourceItemID string, media domain.MediaType) string {
	t.Helper()
	d := issueSource()
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{{SourceID: d.ID, SourceItemID: sourceItemID, MediaType: media, Title: "Issue fixture " + sourceItemID}}}
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", snap); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT id::text FROM inventory_items WHERE source_id=$1 AND source_item_id=$2`, d.ID, sourceItemID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIssuesAreSharedPerItemKindAndPartAndSurviveTheItemLeavingIntegration(t *testing.T) {
	ctx, s, first := setupDraftStore(t)
	second := integrationUser(t, ctx, s)
	svc := issues.New(s.Issues())
	item := libraryItem(t, ctx, s, "series-"+newUUID()[:8], domain.MediaTV)
	report := func(user string, kind domain.IssueKind, note string, part domain.Selection) issues.ReportResult {
		res, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: user, Role: "viewer"}, ItemID: item, Kind: kind, Description: note, Part: part})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	one := report(first, domain.IssueSubtitles, "out of sync", domain.Selection{Seasons: []int{2}})
	if one.Joined || one.Issue.Reporters != 1 || one.Issue.Status != domain.IssueOpen || one.Issue.Description != "out of sync" {
		t.Fatalf("first report: %+v", one)
	}
	// The same thing reported by someone else, in a different order, joins the open Issue.
	two := report(second, domain.IssueSubtitles, "same here", domain.Selection{Seasons: []int{2}})
	if !two.Joined || two.Issue.ID != one.Issue.ID || two.Issue.Reporters != 2 {
		t.Fatalf("second report must join: %+v", two)
	}
	// A different kind, or a different part, is its own Issue.
	if other := report(second, domain.IssueAudio, "", domain.Selection{Seasons: []int{2}}); other.Issue.ID == one.Issue.ID {
		t.Fatal("a different kind must open a separate Issue")
	}
	if other := report(second, domain.IssueSubtitles, "", domain.Selection{Seasons: []int{3}}); other.Issue.ID == one.Issue.ID {
		t.Fatal("a different part must open a separate Issue")
	}
	// A reporter may only comment on an Issue they reported; an admin may comment on any.
	stranger := integrationUser(t, ctx, s)
	if err := svc.Comment(ctx, issues.Actor{ID: stranger, Role: "requester"}, one.Issue.ID, "hi"); !errors.Is(err, issues.ErrForbidden) {
		t.Fatalf("a non-reporter commenting: %v", err)
	}
	if err := svc.Comment(ctx, issues.Actor{ID: stranger, Role: "admin"}, one.Issue.ID, "looking"); err != nil {
		t.Fatalf("admin comment: %v", err)
	}
	// Leaving drops the reporter from the count and does not close the Issue.
	if err := svc.Leave(ctx, issues.Actor{ID: second, Role: "viewer"}, one.Issue.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, one.Issue.ID); got.Reporters != 1 || got.Status != domain.IssueOpen {
		t.Fatalf("after leaving: %+v", got)
	}
	// Resolving frees the slot: the next report for the same thing opens a new Issue.
	admin := issues.Actor{ID: stranger, Role: "admin"}
	if _, err := svc.Transition(ctx, admin, one.Issue.ID, domain.IssueResolved, "re-synced"); err != nil {
		t.Fatal(err)
	}
	if again := report(first, domain.IssueSubtitles, "back again", domain.Selection{Seasons: []int{2}}); again.Joined || again.Issue.ID == one.Issue.ID {
		t.Fatalf("a resolved Issue must not be joined: %+v", again)
	}
	if reopened, err := svc.Transition(ctx, admin, one.Issue.ID, domain.IssueOpen, ""); !errors.Is(err, issues.ErrTransition) {
		t.Fatalf("reopening into a slot another open Issue now holds must be refused cleanly, got %+v %v", reopened, err)
	}
	// The item leaves its library: the Issue stays open and says so.
	d := issueSource()
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().Add(time.Second).UTC(), Items: []inventory.Item{{SourceID: d.ID, SourceItemID: "another-" + newUUID()[:8], MediaType: domain.MediaTV, Title: "Other"}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := svc.Get(ctx, one.Issue.ID)
	if err != nil || !gone.GoneFromLibrary {
		t.Fatalf("gone = %+v err %v", gone, err)
	}
}

func TestReportsAreRateLimitedPerUserIntegration(t *testing.T) {
	ctx, s, user := setupDraftStore(t)
	svc := issues.New(s.Issues())
	for i := 0; i < issues.ReportsPerHour; i++ {
		item := libraryItem(t, ctx, s, "limit-"+newUUID()[:8], domain.MediaMovie)
		if _, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: user, Role: "viewer"}, ItemID: item, Kind: domain.IssueVideo}); err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
	}
	item := libraryItem(t, ctx, s, "limit-"+newUUID()[:8], domain.MediaMovie)
	if _, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: user, Role: "viewer"}, ItemID: item, Kind: domain.IssueVideo}); !errors.Is(err, issues.ErrRateLimited) {
		t.Fatalf("the report over the limit: %v", err)
	}
}
