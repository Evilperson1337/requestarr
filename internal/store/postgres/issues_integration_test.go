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
	ctx, s, first := integrationStore(t)
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
	ctx, s, user := integrationStore(t)
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

func TestIssueNotificationsReachAdminsAndReportersOnlyIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	admin, reporter, leaver, bystander := integrationUser(t, ctx, s), integrationUser(t, ctx, s), integrationUser(t, ctx, s), integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	svc := issues.New(s.Issues())
	item := libraryItem(t, ctx, s, "notify-"+newUUID()[:8], domain.MediaMovie)
	count := func(user, like string) int {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE user_id=$1 AND channel='in_app' AND event_key LIKE $2`, user, like).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	outbox := func(like string) int {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE event_key LIKE $1`, like).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	res, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: reporter, Role: "viewer"}, ItemID: item, Kind: domain.IssueSubtitles, Description: "late"})
	if err != nil {
		t.Fatal(err)
	}
	if count(admin, "issue.reported:%") != 1 || count(reporter, "issue.reported:%") != 0 || count(bystander, "issue.reported:%") != 0 {
		t.Fatal("a new Issue must notify admins and nobody else")
	}
	if outbox("issue.reported:%") < 1 {
		t.Fatal("a new Issue must reach the global Apprise route")
	}
	// A second reporter joins, then leaves; only the one who stayed hears updates.
	if _, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: leaver, Role: "viewer"}, ItemID: item, Kind: domain.IssueSubtitles}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Leave(ctx, issues.Actor{ID: leaver, Role: "viewer"}, res.Issue.ID); err != nil {
		t.Fatal(err)
	}
	// A reporter's own comment tells nobody; an admin's reaches the reporters.
	if err := svc.Comment(ctx, issues.Actor{ID: reporter, Role: "viewer"}, res.Issue.ID, "still bad"); err != nil {
		t.Fatal(err)
	}
	if count(admin, "issue.commented:%") != 0 {
		t.Fatal("a reporter's comment is not an admin notification")
	}
	if err := svc.Comment(ctx, issues.Actor{ID: admin, Role: "admin"}, res.Issue.ID, "looking into it"); err != nil {
		t.Fatal(err)
	}
	if count(reporter, "issue.commented:%") != 1 || count(leaver, "issue.commented:%") != 0 || count(admin, "issue.commented:%") != 0 {
		t.Fatal("an admin comment must reach current reporters, not the one who left or the admin")
	}
	if _, err := svc.Transition(ctx, issues.Actor{ID: admin, Role: "admin"}, res.Issue.ID, domain.IssueResolved, "fixed"); err != nil {
		t.Fatal(err)
	}
	if count(reporter, "issue.resolved:%") != 1 || count(leaver, "issue.resolved:%") != 0 || count(bystander, "issue.resolved:%") != 0 {
		t.Fatal("resolving must reach the reporters who stayed")
	}
	if outbox("issue.resolved:%") < 1 {
		t.Fatal("a status change must reach the global Apprise route")
	}
	if outbox("issue.commented:%") != 0 {
		t.Fatal("comments never go to the global Apprise route")
	}
}

func TestIssueListDetailAndCountsRespectWhoIsAskingIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	mine, theirs, admin := integrationUser(t, ctx, s), integrationUser(t, ctx, s), integrationUser(t, ctx, s)
	svc := issues.New(s.Issues())
	item := libraryItem(t, ctx, s, "list-"+newUUID()[:8], domain.MediaMovie)
	a, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: mine, Role: "viewer"}, ItemID: item, Kind: domain.IssueVideo, Description: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: theirs, Role: "viewer"}, ItemID: item, Kind: domain.IssueAudio, Description: "theirs"})
	if err != nil {
		t.Fatal(err)
	}
	adminActor, mineActor := issues.Actor{ID: admin, Role: "admin"}, issues.Actor{ID: mine, Role: "viewer"}
	all, err := svc.List(ctx, adminActor, domain.IssueQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	has := func(page domain.IssuePage, id string) bool {
		for _, it := range page.Items {
			if it.ID == id {
				return true
			}
		}
		return false
	}
	if !has(all, a.Issue.ID) || !has(all, b.Issue.ID) {
		t.Fatal("an admin sees every Issue")
	}
	own, _ := svc.List(ctx, mineActor, domain.IssueQuery{Limit: 100})
	if !has(own, a.Issue.ID) || has(own, b.Issue.ID) {
		t.Fatal("anyone else sees only the Issues they reported")
	}
	if filtered, _ := svc.List(ctx, adminActor, domain.IssueQuery{Limit: 100, Kind: domain.IssueAudio, Status: domain.IssueOpen, MediaType: domain.MediaMovie}); !has(filtered, b.Issue.ID) || has(filtered, a.Issue.ID) {
		t.Fatal("kind, status and media type filters apply together")
	}
	if _, err := svc.Detail(ctx, mineActor, b.Issue.ID); !errors.Is(err, issues.ErrNotFound) {
		t.Fatalf("someone else's Issue must not exist for me: %v", err)
	}
	d, err := svc.Detail(ctx, mineActor, a.Issue.ID)
	if err != nil || !d.YouReported || len(d.Reporters) != 1 || d.Reporters[0].UserID != mine || len(d.Events) != 1 || d.Events[0].Type != "reported" {
		t.Fatalf("detail = %+v err %v", d, err)
	}
	if d, _ := svc.Detail(ctx, adminActor, b.Issue.ID); len(d.Reporters) != 1 || d.Reporters[0].Note != "theirs" {
		t.Fatalf("an admin sees reporters and their notes: %+v", d.Reporters)
	}
	if n, _ := svc.OpenCount(ctx, mineActor); n != 1 {
		t.Fatalf("my open count = %d", n)
	}
	// Pages are stable and do not repeat.
	first, _ := svc.List(ctx, adminActor, domain.IssueQuery{Limit: 1})
	if first.NextCursor == nil || len(first.Items) != 1 {
		t.Fatalf("first page = %+v", first)
	}
	second, err := svc.List(ctx, adminActor, domain.IssueQuery{Limit: 1, After: *first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second page = %+v err %v", second, err)
	}
	if _, err := svc.List(ctx, adminActor, domain.IssueQuery{After: "garbage"}); !errors.Is(err, issues.ErrBadCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
}

func TestAnIssueInProgressStillHoldsItsSlotAndCountsIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	admin, a, b := integrationUser(t, ctx, s), integrationUser(t, ctx, s), integrationUser(t, ctx, s)
	svc := issues.New(s.Issues())
	item := libraryItem(t, ctx, s, "wip-"+newUUID()[:8], domain.MediaMovie)
	first, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: a, Role: "viewer"}, ItemID: item, Kind: domain.IssueVideo})
	if err != nil {
		t.Fatal(err)
	}
	adminActor := issues.Actor{ID: admin, Role: "admin"}
	moved, err := svc.Transition(ctx, adminActor, first.Issue.ID, domain.IssueInProgress, "re-encoding it")
	if err != nil || moved.Status != domain.IssueInProgress || moved.ResolvedAt != nil {
		t.Fatalf("in progress: %+v err %v", moved, err)
	}
	// Someone else hitting the same problem joins it rather than opening a duplicate.
	if again, err := svc.Report(ctx, issues.ReportInput{Actor: issues.Actor{ID: b, Role: "viewer"}, ItemID: item, Kind: domain.IssueVideo}); err != nil || !again.Joined || again.Issue.ID != first.Issue.ID {
		t.Fatalf("an in-progress Issue must be joined: %+v %v", again, err)
	}
	if n, _ := svc.OpenCount(ctx, adminActor); n < 1 {
		t.Fatalf("in-progress Issues still need an administrator: %d", n)
	}
	var notified int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE user_id=$1 AND event_key LIKE 'issue.in_progress:%'`, a).Scan(&notified); err != nil || notified != 1 {
		t.Fatalf("the reporter must hear it is being worked on: %d %v", notified, err)
	}
	if done, err := svc.Transition(ctx, adminActor, first.Issue.ID, domain.IssueResolved, ""); err != nil || done.ResolvedAt == nil {
		t.Fatalf("resolve: %+v %v", done, err)
	}
	// A resolved Issue can be put straight back in progress, and the library item says so.
	if back, err := svc.Transition(ctx, adminActor, first.Issue.ID, domain.IssueInProgress, ""); err != nil || back.ResolvedAt != nil || back.Title == "" {
		t.Fatalf("back to work: %+v %v", back, err)
	}
	detail, _ := svc.Detail(ctx, adminActor, first.Issue.ID)
	if detail.Issue.SourceKind == "" || detail.Issue.LibraryItemID == "" {
		t.Fatalf("an Issue carries its item's source: %+v", detail.Issue)
	}
}
