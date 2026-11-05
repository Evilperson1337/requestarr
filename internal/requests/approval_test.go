package requests

import (
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
)

func TestCreateStartsApprovedOnlyForAutoApprovedTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		media  domain.MediaType
		policy []domain.MediaType
		want   domain.RequestStatus
	}{
		{"no policy", domain.MediaMovie, nil, domain.RequestRequested},
		{"movie auto-approved", domain.MediaMovie, []domain.MediaType{domain.MediaMovie}, domain.RequestApproved},
		{"audiobook auto-approved", domain.MediaAudiobook, []domain.MediaType{domain.MediaAudiobook, domain.MediaTV}, domain.RequestApproved},
		{"other type waits", domain.MediaTV, []domain.MediaType{domain.MediaMovie}, domain.RequestRequested},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{created: true, mediaType: tc.media, autoTypes: tc.policy}
			got, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "user", Role: "requester"}, CatalogID: "item", Platforms: domain.PlatformSelection{Any: true}})
			if err != nil {
				t.Fatal(err)
			}
			if repo.request.Status != tc.want || got.Request.Status != tc.want {
				t.Fatalf("stored %q, returned %q, want %q", repo.request.Status, got.Request.Status, tc.want)
			}
		})
	}
}

func TestCreateFallsBackToManualApprovalWhenPolicyCannotBeRead(t *testing.T) {
	repo := &fakeRepository{created: true, mediaType: domain.MediaMovie, autoErr: errors.New("db down")}
	if _, err := New(repo).Create(context.Background(), CreateInput{Actor: Actor{ID: "user", Role: "requester"}, CatalogID: "item"}); err != nil {
		t.Fatal(err)
	}
	if repo.request.Status != domain.RequestRequested {
		t.Fatalf("status %q, want requested", repo.request.Status)
	}
}

func TestAwaitingApprovalCountIsAdminOnly(t *testing.T) {
	svc := New(&fakeRepository{awaiting: 4})
	if n, err := svc.AwaitingApprovalCount(context.Background(), Actor{ID: "a", Role: "admin"}); err != nil || n != 4 {
		t.Fatalf("admin n=%d err=%v", n, err)
	}
	for _, role := range []string{"requester", "viewer", ""} {
		if _, err := svc.AwaitingApprovalCount(context.Background(), Actor{ID: "u", Role: role}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("role %q err=%v", role, err)
		}
	}
}
