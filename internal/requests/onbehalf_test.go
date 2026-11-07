package requests

import (
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
)

func onBehalf(actorRole string, repo *fakeRepository) (CreateResult, error) {
	return New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "boss", Role: actorRole}, OnBehalfOf: "alice", CatalogID: "item", Platforms: domain.PlatformSelection{Any: true}})
}

func TestOnBehalfMakesThePersonTheRequesterAndRecordsWhoActed(t *testing.T) {
	repo := &fakeRepository{created: true, userRoles: map[string]string{"alice": "requester"}}
	got, err := onBehalf("admin", repo)
	if err != nil || !got.Created {
		t.Fatalf("got %#v, err %v", got, err)
	}
	if repo.request.RequestedBy != "alice" || repo.request.MadeOnBehalfBy != "boss" {
		t.Fatalf("request = %#v", repo.request)
	}
	if repo.subscription.UserID != "alice" || repo.subscription.Source != domain.SubscriptionRequester {
		t.Fatalf("subscription = %#v", repo.subscription)
	}
}

func TestOnBehalfIsRefusedForEveryRoleThatCannotManageRequests(t *testing.T) {
	for _, role := range []string{"viewer", "requester"} {
		repo := &fakeRepository{created: true, userRoles: map[string]string{"alice": "requester"}}
		if _, err := onBehalf(role, repo); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s: error = %v, want ErrForbidden", role, err)
		}
		if repo.request.CatalogItemID != "" {
			t.Fatalf("%s: repository called", role)
		}
	}
}

func TestOnBehalfWorksForSuperuser(t *testing.T) {
	repo := &fakeRepository{created: true, userRoles: map[string]string{"alice": "requester"}}
	if _, err := onBehalf("superuser", repo); err != nil || repo.request.RequestedBy != "alice" {
		t.Fatalf("err %v request %#v", err, repo.request)
	}
}

func TestOnBehalfNeedsAKnownPersonWhoMayRequest(t *testing.T) {
	for name, roles := range map[string]map[string]string{"unknown person": {}, "viewer": {"alice": "viewer"}} {
		repo := &fakeRepository{created: true, userRoles: roles}
		if _, err := onBehalf("admin", repo); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: error = %v, want ErrInvalid", name, err)
		}
		if repo.request.CatalogItemID != "" {
			t.Fatalf("%s: request was created", name)
		}
	}
}

func TestOnBehalfSubscribesThePersonToAnExistingRequest(t *testing.T) {
	existing := domain.Request{ID: "existing", RequestedBy: "original", Status: domain.RequestRequested}
	repo := &fakeRepository{existing: existing, userRoles: map[string]string{"alice": "requester"}}
	got, err := onBehalf("admin", repo)
	if err != nil || got.Created || got.Request.ID != "existing" {
		t.Fatalf("got %#v err %v", got, err)
	}
	if repo.subscribedUser != "alice" || repo.subscribedRequest != "existing" {
		t.Fatalf("subscribed %q to %q", repo.subscribedUser, repo.subscribedRequest)
	}
}

func TestOnBehalfAppliesTheApprovalPolicyAsIfThePersonAsked(t *testing.T) {
	repo := &fakeRepository{created: true, mediaType: domain.MediaMovie, autoTypes: []domain.MediaType{domain.MediaMovie}, userRoles: map[string]string{"alice": "requester"}}
	if _, err := onBehalf("admin", repo); err != nil || repo.request.Status != domain.RequestApproved {
		t.Fatalf("status = %q err %v", repo.request.Status, err)
	}
	repo = &fakeRepository{created: true, mediaType: domain.MediaMovie, userRoles: map[string]string{"alice": "requester"}}
	if _, err := onBehalf("admin", repo); err != nil || repo.request.Status != domain.RequestRequested {
		t.Fatalf("status = %q err %v", repo.request.Status, err)
	}
}
