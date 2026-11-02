package issues

import (
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
)

type fakeRepo struct {
	item      LibraryItem
	itemErr   error
	status    domain.IssueStatus
	reported  *ReportInput
	transited *domain.IssueStatus
	comments  []string
}

func (f *fakeRepo) LibraryItem(context.Context, string) (LibraryItem, error) {
	return f.item, f.itemErr
}
func (f *fakeRepo) ReportOrJoin(_ context.Context, in ReportInput, _ int) (domain.Issue, bool, error) {
	f.reported = &in
	return domain.Issue{ID: "i1", Status: domain.IssueOpen}, false, nil
}
func (f *fakeRepo) Get(context.Context, string) (domain.Issue, error) {
	return domain.Issue{ID: "i1", Status: f.status}, nil
}
func (f *fakeRepo) Comment(_ context.Context, _, _ string, _ bool, text string) error {
	f.comments = append(f.comments, text)
	return nil
}
func (f *fakeRepo) Transition(_ context.Context, _ string, to domain.IssueStatus, _, _ string) (domain.Issue, error) {
	f.transited = &to
	return domain.Issue{ID: "i1", Status: to}, nil
}
func (f *fakeRepo) Leave(context.Context, string, string) error { return nil }
func (f *fakeRepo) List(context.Context, string, bool, domain.IssueQuery) (domain.IssuePage, error) {
	return domain.IssuePage{}, nil
}
func (f *fakeRepo) Detail(context.Context, string, string, bool) (domain.IssueDetail, error) {
	return domain.IssueDetail{}, nil
}
func (f *fakeRepo) OpenCount(context.Context, string, bool) (int, error) { return 0, nil }

func TestEverySignedInRoleCanReportButNobodyElse(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		repo := &fakeRepo{item: LibraryItem{MediaType: domain.MediaMovie}}
		if _, err := New(repo).Report(context.Background(), ReportInput{Actor: Actor{ID: "u", Role: role}, ItemID: "x", Kind: domain.IssueSubtitles}); err != nil || repo.reported == nil {
			t.Errorf("%s: %v", role, err)
		}
	}
	for _, actor := range []Actor{{ID: "u", Role: ""}, {ID: "u", Role: "guest"}, {ID: "", Role: "viewer"}} {
		repo := &fakeRepo{item: LibraryItem{MediaType: domain.MediaMovie}}
		if _, err := New(repo).Report(context.Background(), ReportInput{Actor: actor, ItemID: "x", Kind: domain.IssueSubtitles}); !errors.Is(err, ErrForbidden) || repo.reported != nil {
			t.Errorf("%+v must be forbidden: %v", actor, err)
		}
	}
}

func TestReportIsValidatedAgainstTheItemsMediaType(t *testing.T) {
	repo := &fakeRepo{item: LibraryItem{MediaType: domain.MediaGame}}
	svc := New(repo)
	actor := Actor{ID: "u", Role: "viewer"}
	if _, err := svc.Report(context.Background(), ReportInput{Actor: actor, ItemID: "x", Kind: domain.IssueSubtitles}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a game has no subtitles: %v", err)
	}
	if _, err := svc.Report(context.Background(), ReportInput{Actor: actor, ItemID: "x", Kind: domain.IssueOther}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other needs a description: %v", err)
	}
	if _, err := svc.Report(context.Background(), ReportInput{Actor: actor, ItemID: "x", Kind: domain.IssueWrongRegion, Description: " PAL disc "}); err != nil || repo.reported.Description != "PAL disc" {
		t.Fatalf("a game kind: %v %+v", err, repo.reported)
	}
	repo.itemErr = ErrNotFound
	if _, err := svc.Report(context.Background(), ReportInput{Actor: actor, ItemID: "x", Kind: domain.IssueWrongRegion}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown item: %v", err)
	}
}

func TestOnlyAdminsMoveAnIssueAndOnlyAlongAllowedEdges(t *testing.T) {
	repo := &fakeRepo{status: domain.IssueOpen}
	svc := New(repo)
	for _, role := range []string{"viewer", "requester"} {
		if _, err := svc.Transition(context.Background(), Actor{ID: "u", Role: role}, "i1", domain.IssueResolved, ""); !errors.Is(err, ErrForbidden) || repo.transited != nil {
			t.Errorf("%s may not resolve: %v", role, err)
		}
	}
	admin := Actor{ID: "a", Role: "admin"}
	if _, err := svc.Transition(context.Background(), admin, "i1", domain.IssueResolved, "fixed"); err != nil || *repo.transited != domain.IssueResolved {
		t.Fatalf("admin resolve: %v", err)
	}
	repo.status = domain.IssueResolved
	if _, err := svc.Transition(context.Background(), admin, "i1", domain.IssueClosed, ""); !errors.Is(err, ErrTransition) {
		t.Fatalf("resolved -> closed must be refused: %v", err)
	}
}

func TestCommentsAreTrimmedAndBounded(t *testing.T) {
	repo := &fakeRepo{}
	svc := New(repo)
	actor := Actor{ID: "u", Role: "requester"}
	if err := svc.Comment(context.Background(), actor, "i1", "  hello  "); err != nil || repo.comments[0] != "hello" {
		t.Fatalf("%v %v", err, repo.comments)
	}
	for _, bad := range []string{"", "   ", string(make([]rune, MaxComment+1)), "a\x00b"} {
		if err := svc.Comment(context.Background(), actor, "i1", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q must be invalid", bad)
		}
	}
}
