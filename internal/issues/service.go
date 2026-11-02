// Package issues holds the Issue use cases (D030): people report problems with
// Library items, admins resolve them. It never touches Requests or any download.
package issues

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/domain"
)

var (
	ErrForbidden   = errors.New("not permitted")
	ErrInvalid     = errors.New("invalid issue")
	ErrNotFound    = errors.New("not found")
	ErrRateLimited = errors.New("too many recent reports")
	ErrTransition  = errors.New("that status change is not allowed")
)

// ReportsPerHour is how many new reports (or joins) one user may make an hour.
const ReportsPerHour = 10

// MaxComment bounds an admin or reporter comment.
const MaxComment = 2000

type Actor struct{ ID, Role string }

// LibraryItem is what a report needs to know about the item it is about.
type LibraryItem struct {
	ID         string
	Title      string
	MediaType  domain.MediaType
	SourceName string
}

type ReportInput struct {
	Actor       Actor
	ItemID      string
	Kind        domain.IssueKind
	Description string
	Part        domain.Selection
}

type Repository interface {
	LibraryItem(ctx context.Context, id string) (LibraryItem, error)
	// ReportOrJoin opens an Issue, or adds the reporter to the open Issue for the
	// same item, kind and part. It enforces the per-user hourly limit atomically.
	ReportOrJoin(ctx context.Context, in ReportInput, limit int) (issue domain.Issue, joined bool, err error)
	Get(ctx context.Context, id string) (domain.Issue, error)
	// Comment adds a note; a non-admin may only comment on an Issue they reported.
	Comment(ctx context.Context, issueID, userID string, admin bool, text string) error
	Transition(ctx context.Context, issueID string, to domain.IssueStatus, actorID, comment string) (domain.Issue, error)
	Leave(ctx context.Context, issueID, userID string) error
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

type ReportResult struct {
	Issue domain.Issue
	// Joined is true when the report was added to an Issue that was already open.
	Joined bool
}

func signedIn(a Actor) bool {
	return a.ID != "" && (a.Role == "viewer" || a.Role == "requester" || a.Role == "admin")
}

// Report records a problem with a Library item. Every signed-in role may report.
func (s *Service) Report(ctx context.Context, in ReportInput) (ReportResult, error) {
	if !signedIn(in.Actor) {
		return ReportResult{}, ErrForbidden
	}
	item, err := s.repo.LibraryItem(ctx, in.ItemID)
	if err != nil {
		return ReportResult{}, err
	}
	in.Description = strings.TrimSpace(in.Description)
	if err := domain.ValidateIssue(item.MediaType, in.Kind, in.Description, in.Part); err != nil {
		return ReportResult{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	issue, joined, err := s.repo.ReportOrJoin(ctx, in, ReportsPerHour)
	if err != nil {
		return ReportResult{}, err
	}
	return ReportResult{Issue: issue, Joined: joined}, nil
}

func validComment(text string) (string, bool) {
	text = strings.TrimSpace(text)
	return text, text != "" && utf8.ValidString(text) && utf8.RuneCountInString(text) <= MaxComment && !strings.ContainsRune(text, 0)
}

// Comment adds a note. Admins may comment on any Issue; others only on Issues they reported.
func (s *Service) Comment(ctx context.Context, actor Actor, issueID, text string) error {
	if !signedIn(actor) {
		return ErrForbidden
	}
	text, ok := validComment(text)
	if !ok {
		return ErrInvalid
	}
	return s.repo.Comment(ctx, issueID, actor.ID, actor.Role == "admin", text)
}

// Transition resolves, closes or reopens an Issue. Only admins may.
func (s *Service) Transition(ctx context.Context, actor Actor, issueID string, to domain.IssueStatus, comment string) (domain.Issue, error) {
	if actor.Role != "admin" || actor.ID == "" {
		return domain.Issue{}, ErrForbidden
	}
	comment = strings.TrimSpace(comment)
	if comment != "" {
		var ok bool
		if comment, ok = validComment(comment); !ok {
			return domain.Issue{}, ErrInvalid
		}
	}
	issue, err := s.repo.Get(ctx, issueID)
	if err != nil {
		return domain.Issue{}, err
	}
	if !domain.CanTransitionIssue(issue.Status, to) {
		return domain.Issue{}, ErrTransition
	}
	return s.repo.Transition(ctx, issueID, to, actor.ID, comment)
}

// Leave stops a reporter's notifications about an Issue; it does not close it.
func (s *Service) Leave(ctx context.Context, actor Actor, issueID string) error {
	if !signedIn(actor) {
		return ErrForbidden
	}
	return s.repo.Leave(ctx, issueID, actor.ID)
}

func (s *Service) Get(ctx context.Context, issueID string) (domain.Issue, error) {
	return s.repo.Get(ctx, issueID)
}
