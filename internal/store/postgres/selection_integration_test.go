//go:build integration

package postgres

import (
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

func TestRequestSelectionIsStoredMergedAndFollowedIntegration(t *testing.T) {
	ctx, s, first := setupDraftStore(t)
	second := integrationUser(t, ctx, s)
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaTV, Provider: "tmdb", ProviderID: newUUID(), Title: "Selection fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	seasonOne := domain.Selection{Seasons: []int{1}}
	req, created, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: item.ID, RequestedBy: first, Status: domain.RequestRequested, Selection: seasonOne}, domain.RequestSubscription{UserID: first, Source: domain.SubscriptionRequester})
	if err != nil || !created || !req.Selection.Equal(seasonOne) {
		t.Fatalf("create: %+v created=%v err=%v", req, created, err)
	}
	// A second requester finds the existing Request with its stored selection.
	existing, created, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: item.ID, RequestedBy: second, Status: domain.RequestRequested, Selection: domain.Selection{Seasons: []int{2}}}, domain.RequestSubscription{UserID: second, Source: domain.SubscriptionRequester})
	if err != nil || created || !existing.Selection.Equal(seasonOne) {
		t.Fatalf("existing: %+v created=%v err=%v", existing, created, err)
	}
	merged, changed, err := s.MergeSelection(ctx, req.ID, second, domain.Selection{Seasons: []int{2}, Episodes: []domain.EpisodeRef{{Season: 3, Episode: 4}}})
	if err != nil || !changed || !merged.Selection.Equal(domain.Selection{Seasons: []int{1, 2}, Episodes: []domain.EpisodeRef{{Season: 3, Episode: 4}}}) {
		t.Fatalf("merge: %+v changed=%v err=%v", merged, changed, err)
	}
	var version int64
	var subscribed bool
	if err := s.pool.QueryRow(ctx, `SELECT r.version,EXISTS(SELECT 1 FROM request_subscriptions WHERE request_id=r.id AND user_id=$2) FROM requests r WHERE r.id=$1`, req.ID, second).Scan(&version, &subscribed); err != nil || version != 2 || !subscribed {
		t.Fatalf("version=%d subscribed=%v err=%v", version, subscribed, err)
	}
	// Merging what is already covered changes nothing.
	if _, changed, err := s.MergeSelection(ctx, req.ID, second, domain.Selection{Seasons: []int{1}}); err != nil || changed {
		t.Fatalf("covered merge changed=%v err=%v", changed, err)
	}
	page, err := s.List(ctx, first, requests.ListQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range page.Items {
		if listed.ID == req.ID && listed.Selection.Equal(merged.Selection) {
			return
		}
	}
	t.Fatal("the listed Request does not carry its merged selection")
}
