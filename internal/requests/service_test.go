package requests

import (
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
)

type fakeRepository struct {
	request      domain.Request
	existing     domain.Request
	subscription domain.RequestSubscription
	created      bool
	platformErr  error
	mediaType    domain.MediaType
	mediaErr     error
}

func (f *fakeRepository) CatalogMediaType(context.Context, string) (domain.MediaType, error) {
	return f.mediaType, f.mediaErr
}

func (f *fakeRepository) CreateOrGet(_ context.Context, r domain.Request, s domain.RequestSubscription) (domain.Request, bool, error) {
	f.request, f.subscription = r, s
	if !f.created && f.existing.ID != "" {
		return f.existing, false, nil
	}
	return r, f.created, nil
}

func (f *fakeRepository) List(context.Context, string, bool) ([]domain.Request, error) {
	return nil, nil
}

func (f *fakeRepository) ValidatePlatforms(context.Context, string, domain.PlatformSelection) error {
	return f.platformErr
}
func (f *fakeRepository) CatalogPlatforms(context.Context, string) ([]domain.Platform, error) {
	return nil, nil
}
func (f *fakeRepository) Transition(_ context.Context, in TransitionInput) (domain.Request, bool, error) {
	return domain.Request{ID: in.RequestID, Status: in.ToStatus, Version: in.ExpectedVersion + 1}, true, nil
}
func (f *fakeRepository) Subscribe(context.Context, string, string) (bool, error)   { return true, nil }
func (f *fakeRepository) Unsubscribe(context.Context, string, string) (bool, error) { return true, nil }

func TestSubscriptionAllowedForAnyAuthenticatedRole(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		repo := &fakeRepository{}
		changed, err := New(repo).Subscribe(context.Background(), Actor{ID: "user", Role: role}, "request")
		if err != nil || !changed {
			t.Fatalf("role %s: changed=%v err=%v", role, changed, err)
		}
	}
}

func TestTransitionRequiresAdminAndValidExpectedOperation(t *testing.T) {
	repo := &fakeRepository{}
	svc := New(repo)
	base := TransitionInput{Actor: Actor{ID: "admin", Role: "admin"}, RequestID: "request", ToStatus: domain.RequestApproved, ExpectedVersion: 1, OperationID: "op"}
	if _, err := svc.Transition(context.Background(), TransitionInput{Actor: Actor{ID: "u", Role: "requester"}, RequestID: "request", ToStatus: domain.RequestApproved, ExpectedVersion: 1, OperationID: "op"}); !errors.Is(err, ErrTransitionForbidden) {
		t.Fatalf("non-admin error=%v", err)
	}
	for _, mutate := range []func(*TransitionInput){func(i *TransitionInput) { i.ToStatus = domain.RequestAvailable }, func(i *TransitionInput) { i.OperationID = "" }, func(i *TransitionInput) { i.ExpectedVersion = 0 }} {
		in := base
		mutate(&in)
		if _, err := svc.Transition(context.Background(), in); !errors.Is(err, ErrTransitionInvalid) {
			t.Fatalf("invalid transition error=%v", err)
		}
	}
	got, err := svc.Transition(context.Background(), base)
	if err != nil || got.Request.Status != domain.RequestApproved || got.Request.Version != 2 {
		t.Fatalf("result=%#v error=%v", got, err)
	}
}

func TestCreateRejectsPlatformsNotInCatalog(t *testing.T) {
	repo := &fakeRepository{platformErr: errors.New("unknown platform key")}
	_, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "user", Role: "requester"}, CatalogID: "game", Platforms: domain.PlatformSelection{PlatformKeys: []string{"igdb:999"}}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if repo.request.CatalogItemID != "" {
		t.Fatal("repository created request for unknown platform")
	}
}

func TestCreateRequiresRequesterRole(t *testing.T) {
	repo := &fakeRepository{}
	_, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "user", Role: "viewer"}, CatalogID: "item"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if repo.request.CatalogItemID != "" {
		t.Fatal("repository called for forbidden actor")
	}
}

func TestCreateCreatesCreatorSubscription(t *testing.T) {
	repo := &fakeRepository{created: true}
	got, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "user", Role: "requester"}, CatalogID: "item", Platforms: domain.PlatformSelection{Any: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Created || repo.subscription.UserID != "user" || repo.subscription.Source != domain.SubscriptionRequester {
		t.Fatalf("create result/subscription = %#v / %#v", got, repo.subscription)
	}
}

func TestCreateReturnsExistingRequestWithoutChangingCreatorSubscription(t *testing.T) {
	existing := domain.Request{ID: "existing", CatalogItemID: "item", RequestedBy: "original", Status: domain.RequestRequested}
	repo := &fakeRepository{existing: existing, created: false}
	got, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "another", Role: "requester"}, CatalogID: "item", Platforms: domain.PlatformSelection{Any: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Created || got.Request.ID != "existing" || got.Request.RequestedBy != "original" {
		t.Fatalf("duplicate create result = %#v", got)
	}
	if repo.request.RequestedBy != "another" {
		t.Fatalf("duplicate lookup input requester = %q, want caller ID", repo.request.RequestedBy)
	}
}

func TestCreateRejectsInvalidInputBeforeRepository(t *testing.T) {
	repo := &fakeRepository{}
	_, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "user", Role: "requester"}, CatalogID: "item", Note: string(make([]byte, 2001)), Platforms: domain.PlatformSelection{Any: true, PlatformKeys: []string{"x"}}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if repo.request.CatalogItemID != "" {
		t.Fatal("repository called for invalid input")
	}
}

func TestCreateRejectsInvalidPlatformSelectionBeforeRepository(t *testing.T) {
	tests := []struct {
		name      string
		selection domain.PlatformSelection
	}{
		{name: "empty explicit selection"},
		{name: "Any with explicit selection", selection: domain.PlatformSelection{Any: true, PlatformKeys: []string{"x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &platformValidationFake{fakeRepository: &fakeRepository{}}
			_, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "user", Role: "requester"}, CatalogID: "item", Platforms: tt.selection})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
			if repo.request.CatalogItemID != "" {
				t.Fatal("repository called for invalid platform selection")
			}
		})
	}
}

type platformValidationFake struct{ *fakeRepository }

func (*platformValidationFake) CatalogMediaType(context.Context, string) (domain.MediaType, error) {
	return "game", nil
}

func TestCreateNormalizesPlatformsForNonGameAndRequiresGameChoice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		media domain.MediaType
		in    domain.PlatformSelection
		valid bool
		want  domain.PlatformSelection
	}{
		{"movie ignores Any", domain.MediaMovie, domain.PlatformSelection{Any: true}, true, domain.PlatformSelection{}},
		{"movie ignores keys", domain.MediaMovie, domain.PlatformSelection{PlatformKeys: []string{"ignored"}}, true, domain.PlatformSelection{}},
		{"game empty invalid", domain.MediaGame, domain.PlatformSelection{}, false, domain.PlatformSelection{}},
		{"game Any valid", domain.MediaGame, domain.PlatformSelection{Any: true}, true, domain.PlatformSelection{Any: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{created: true, mediaType: tc.media}
			_, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "u", Role: "requester"}, CatalogID: "c", Platforms: tc.in})
			if (err == nil) != tc.valid {
				t.Fatalf("error=%v", err)
			}
			if tc.valid && (repo.request.Platforms.Any != tc.want.Any || len(repo.request.Platforms.PlatformKeys) != len(tc.want.PlatformKeys)) {
				t.Fatalf("platform selection=%#v", repo.request.Platforms)
			}
		})
	}
}
