//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

func suggestionFixture(t *testing.T, ctx context.Context, s *Store) (string, string, string, inventory.Descriptor, inventory.Snapshot) {
	t.Helper()
	catalogID, requestID, userID := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	// A unique title keeps these tests independent of catalog rows from other tests.
	title := "The Arrival " + catalogID
	if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET title=$2,year=2016 WHERE id=$1`, catalogID, title); err != nil {
		t.Fatal(err)
	}
	d := inventory.Descriptor{ID: newUUID(), Kind: "plex", Name: "Review library", MediaTypes: []domain.MediaType{domain.MediaMovie}, IdentifierKinds: []string{"tmdb"}}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM library_sources WHERE id=$1`, d.ID) })
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC().Truncate(time.Microsecond), Items: []inventory.Item{{SourceID: d.ID, SourceItemID: "library-item", MediaType: domain.MediaMovie, Title: "  THE: Arrival   " + catalogID, Year: 2016}}}
	return catalogID, requestID, userID, d, snap
}

func TestTitleYearSuggestionDoesNotChangeAvailability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID, _, d, snap := suggestionFixture(t, ctx, s)
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)
	persistAs(t, ctx, s, d, snap)
	suggestions, err := s.ListMatchSuggestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, suggestion := range suggestions {
		if suggestion.CatalogItemID == catalogID {
			found = true
			if suggestion.Confidence != "title_year" || suggestion.Reason != "Normalized title and year match; no stable identifier match." || suggestion.SourceName != "Review library" || suggestion.InventoryYear != 2016 || suggestion.CatalogYear != 2016 {
				t.Fatalf("unexpected suggestion: %+v", suggestion)
			}
		}
	}
	if !found {
		t.Fatal("title/year match was not offered for review")
	}
	if got := readAvailability(t, ctx, s, catalogID); got.evidence != 0 || got.availability != "unknown" || got.available != 0 {
		t.Fatalf("suggestion became evidence: %+v", got)
	}
	if after := readRequestEvidence(t, ctx, s, catalogID, requestID); after != before {
		t.Fatalf("suggestion changed request: before=%+v after=%+v", before, after)
	}
}

func TestAcceptSuggestionUsesNormalAvailabilityPathAndSurvivesSyncs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID, userID, d, snap := suggestionFixture(t, ctx, s)
	persistAs(t, ctx, s, d, snap)
	id := suggestionFor(t, ctx, s, catalogID).ID
	if err := s.ReviewMatchSuggestion(ctx, id, userID, "accept"); err != nil {
		t.Fatal(err)
	}
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"available", 1, 1, 0}) {
		t.Fatalf("accept did not add ordinary evidence: %+v", got)
	}
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)
	if before.status != "available" || before.auditEvents != 1 || before.inApp != 1 || before.catalogEvents != 1 {
		t.Fatalf("accept skipped normal availability path: %+v", before)
	}
	if err := s.ReviewMatchSuggestion(ctx, id, userID, "accept"); err != nil {
		t.Fatalf("retry must succeed: %v", err)
	}
	for _, hours := range []int{25, 50} {
		snap.ObservedAt = snap.ObservedAt.Add(time.Duration(hours) * time.Hour)
		persistAs(t, ctx, s, d, snap)
	}
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"available", 1, 1, 0}) {
		t.Fatalf("accepted mapping was lost on later syncs: %+v", got)
	}
	if after := readRequestEvidence(t, ctx, s, catalogID, requestID); after != before {
		t.Fatalf("retry or resync duplicated availability effects: before=%+v after=%+v", before, after)
	}
	if err := s.ReviewMatchSuggestion(ctx, id, userID, "dismiss"); !errors.Is(err, domain.ErrMatchAlreadyReviewed) {
		t.Fatalf("reversing an accepted decision: %v", err)
	}
	var audit int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM match_suggestion_events WHERE suggestion_id=$1 AND event_type='match.accepted' AND actor_user_id=$2`, id, userID).Scan(&audit); err != nil || audit != 1 {
		t.Fatalf("accept audit count=%d err=%v", audit, err)
	}
}

func suggestionFor(t *testing.T, ctx context.Context, s *Store, catalogID string) domain.MatchSuggestion {
	t.Helper()
	suggestions, err := s.ListMatchSuggestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, suggestion := range suggestions {
		if suggestion.CatalogItemID == catalogID {
			return suggestion
		}
	}
	t.Fatalf("no pending suggestion for %s", catalogID)
	return domain.MatchSuggestion{}
}

func TestDismissSuggestionIsTerminalAndNeverCreatesEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID, userID, d, snap := suggestionFixture(t, ctx, s)
	persistAs(t, ctx, s, d, snap)
	id := suggestionFor(t, ctx, s, catalogID).ID
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)
	for range 2 {
		if err := s.ReviewMatchSuggestion(ctx, id, userID, "dismiss"); err != nil {
			t.Fatal(err)
		}
	}
	snap.ObservedAt = snap.ObservedAt.Add(time.Hour)
	persistAs(t, ctx, s, d, snap)
	assertNoSuggestion(t, ctx, s, catalogID)
	if err := s.ReviewMatchSuggestion(ctx, id, userID, "accept"); !errors.Is(err, domain.ErrMatchAlreadyReviewed) {
		t.Fatalf("dismissed suggestion could be accepted: %v", err)
	}
	if got := readAvailability(t, ctx, s, catalogID); got.evidence != 0 || got.availability != "unknown" {
		t.Fatalf("dismiss changed availability: %+v", got)
	}
	if after := readRequestEvidence(t, ctx, s, catalogID, requestID); after != before {
		t.Fatalf("dismiss changed request: %+v", after)
	}
	var events int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM match_suggestion_events WHERE suggestion_id=$1`, id).Scan(&events); err != nil || events != 2 {
		t.Fatalf("want one suggestion event and one dismissal event: count=%d err=%v", events, err)
	}
}

func TestSuggestionCandidateRuleAndStableIdentifierPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*inventory.Item, string)
		confidence string
		available  bool
	}{
		{"missing year", func(i *inventory.Item, _ string) { i.Year = 0 }, "title_only", false},
		{"different year", func(i *inventory.Item, _ string) { i.Year = 2017 }, "", false},
		{"different media type", func(i *inventory.Item, _ string) { i.MediaType = domain.MediaTV }, "", false},
		{"different title", func(i *inventory.Item, _ string) { i.Title += " sequel" }, "", false},
		{"empty normalized title", func(i *inventory.Item, _ string) { i.Title = "?! " }, "", false},
		{"exact identifier", func(i *inventory.Item, id string) { i.ExternalIDs = map[string]string{"tmdb": id} }, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s := openJobStore(t, ctx)
			catalogID, _, _, d, snap := suggestionFixture(t, ctx, s)
			d.MediaTypes = append(d.MediaTypes, domain.MediaTV)
			tc.change(&snap.Items[0], catalogID)
			persistAs(t, ctx, s, d, snap)
			if tc.confidence != "" {
				got := suggestionFor(t, ctx, s, catalogID)
				if got.Confidence != tc.confidence || !strings.Contains(got.Reason, "year is missing") {
					t.Fatalf("missing year must have lower confidence and a reason: %+v", got)
				}
			} else {
				assertNoSuggestion(t, ctx, s, catalogID)
			}
			if got := readAvailability(t, ctx, s, catalogID); (got.evidence > 0) != tc.available {
				t.Fatalf("unexpected evidence: %+v", got)
			}
		})
	}
}

func TestStaleSuggestionsCannotBeAccepted(t *testing.T) {
	for _, tc := range []string{"absent", "changed title", "changed year", "disabled", "exact match supersedes suggestion"} {
		t.Run(tc, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s := openJobStore(t, ctx)
			catalogID, _, userID, d, snap := suggestionFixture(t, ctx, s)
			persistAs(t, ctx, s, d, snap)
			id := suggestionFor(t, ctx, s, catalogID).ID
			snap.ObservedAt = snap.ObservedAt.Add(time.Hour)
			switch tc {
			case "absent":
				snap.Items = nil
			case "changed title":
				snap.Items[0].Title = "A different movie"
			case "changed year":
				snap.Items[0].Year = 2020
			case "exact match supersedes suggestion":
				snap.Items[0].ExternalIDs = map[string]string{"tmdb": catalogID}
			}
			persistAs(t, ctx, s, d, snap)
			if tc == "disabled" {
				if _, err := s.pool.Exec(ctx, `UPDATE library_sources SET enabled=false WHERE id=$1`, d.ID); err != nil {
					t.Fatal(err)
				}
			}
			assertNoSuggestion(t, ctx, s, catalogID)
			if err := s.ReviewMatchSuggestion(ctx, id, userID, "accept"); !errors.Is(err, domain.ErrMatchStale) {
				t.Fatalf("stale suggestion accepted: %v", err)
			}
			if tc != "exact match supersedes suggestion" {
				if got := readAvailability(t, ctx, s, catalogID); got.evidence != 0 {
					t.Fatalf("stale suggestion created evidence: %+v", got)
				}
			}
		})
	}
}

func assertNoSuggestion(t *testing.T, ctx context.Context, s *Store, catalogID string) {
	t.Helper()
	items, err := s.ListMatchSuggestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.CatalogItemID == catalogID {
			t.Fatalf("unexpected pending suggestion: %+v", item)
		}
	}
}

func TestFailedAndPartialSyncsPreservePendingSuggestion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, _, userID, d, snap := suggestionFixture(t, ctx, s)
	persistAs(t, ctx, s, d, snap)
	before := suggestionFor(t, ctx, s, catalogID)
	if err := s.RecordSyncFailure(ctx, d, snap.ObservedAt.Add(time.Hour), "source unavailable"); err != nil {
		t.Fatal(err)
	}
	partial := snap
	partial.Complete = false
	partial.Items = nil
	if err := s.PersistSnapshot(ctx, d, "http://plex.example", partial); err == nil {
		t.Fatal("partial snapshot was accepted")
	}
	if after := suggestionFor(t, ctx, s, catalogID); after != before {
		t.Fatalf("failed sync changed candidate: before=%+v after=%+v", before, after)
	}
	if err := s.ReviewMatchSuggestion(ctx, before.ID, userID, "accept"); err != nil {
		t.Fatalf("last-known candidate could not be reviewed: %v", err)
	}
}

func TestAcceptedSuggestionUsesNormalAbsenceSafeguards(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, _, userID, d, snap := suggestionFixture(t, ctx, s)
	persistAs(t, ctx, s, d, snap)
	if err := s.ReviewMatchSuggestion(ctx, suggestionFor(t, ctx, s, catalogID).ID, userID, "accept"); err != nil {
		t.Fatal(err)
	}
	// An unrelated item keeps the snapshot non-empty. Accepted mappings do not
	// bypass the existing two-complete-absences safeguard.
	persistAs(t, ctx, s, d, without(d, catalogID, snap.ObservedAt.Add(time.Hour)))
	if got := readAvailability(t, ctx, s, catalogID); got.evidence != 1 || got.availability != "available" {
		t.Fatalf("one absence removed accepted evidence: %+v", got)
	}
	persistAs(t, ctx, s, d, without(d, catalogID, snap.ObservedAt.Add(26*time.Hour)))
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"missing", 0, 1, 1}) {
		t.Fatalf("two absences did not reconcile accepted evidence: %+v", got)
	}
	snap.ObservedAt = snap.ObservedAt.Add(30 * time.Hour)
	persistAs(t, ctx, s, d, snap)
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"available", 1, 2, 1}) {
		t.Fatalf("accepted mapping failed to restore returning evidence: %+v", got)
	}
	assertNoSuggestion(t, ctx, s, catalogID)
}

func TestConcurrentMatchReviewsHaveOneFinalDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, _, userID, d, snap := suggestionFixture(t, ctx, s)
	persistAs(t, ctx, s, d, snap)
	id := suggestionFor(t, ctx, s, catalogID).ID
	start := make(chan struct{})
	type result struct {
		decision string
		err      error
	}
	results := make(chan result, 2)
	for _, decision := range []string{"accept", "dismiss"} {
		go func() {
			<-start
			results <- result{decision, s.ReviewMatchSuggestion(ctx, id, userID, decision)}
		}()
	}
	close(start)
	winner := ""
	for range 2 {
		result := <-results
		if result.err == nil {
			if winner != "" {
				t.Fatal("both opposing decisions succeeded")
			}
			winner = result.decision
		} else if !errors.Is(result.err, domain.ErrMatchAlreadyReviewed) {
			t.Fatalf("unexpected concurrent review error: %v", result.err)
		}
	}
	if winner == "" {
		t.Fatal("neither review succeeded")
	}
	if got := readAvailability(t, ctx, s, catalogID); (got.evidence == 1) != (winner == "accept") {
		t.Fatalf("evidence disagrees with final decision %s: %+v", winner, got)
	}
	var events int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM match_suggestion_events WHERE suggestion_id=$1 AND event_type IN ('match.accepted','match.dismissed')`, id).Scan(&events); err != nil || events != 1 {
		t.Fatalf("expected exactly one review audit: count=%d err=%v", events, err)
	}
}
