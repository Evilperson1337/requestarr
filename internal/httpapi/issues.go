package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/issues"
)

// IssueService is what the Issue routes need (D030). Its methods carry an Issue
// prefix only so one test double can also satisfy the other services.
type IssueService interface {
	ReportIssue(ctx context.Context, in issues.ReportInput) (issues.ReportResult, error)
	ListIssues(ctx context.Context, actor issues.Actor, q domain.IssueQuery) (domain.IssuePage, error)
	IssueDetail(ctx context.Context, actor issues.Actor, id string) (domain.IssueDetail, error)
	CommentOnIssue(ctx context.Context, actor issues.Actor, id, text string) error
	TransitionIssue(ctx context.Context, actor issues.Actor, id string, to domain.IssueStatus, comment string) (domain.Issue, error)
	LeaveIssue(ctx context.Context, actor issues.Actor, id string) error
	OpenIssueCount(ctx context.Context, actor issues.Actor) (int, error)
}

// NewIssueService adapts the Issue use cases to the HTTP layer.
func NewIssueService(svc *issues.Service) IssueService { return issueAdapter{svc} }

type issueAdapter struct{ svc *issues.Service }

func (a issueAdapter) ReportIssue(ctx context.Context, in issues.ReportInput) (issues.ReportResult, error) {
	return a.svc.Report(ctx, in)
}
func (a issueAdapter) ListIssues(ctx context.Context, actor issues.Actor, q domain.IssueQuery) (domain.IssuePage, error) {
	return a.svc.List(ctx, actor, q)
}
func (a issueAdapter) IssueDetail(ctx context.Context, actor issues.Actor, id string) (domain.IssueDetail, error) {
	return a.svc.Detail(ctx, actor, id)
}
func (a issueAdapter) CommentOnIssue(ctx context.Context, actor issues.Actor, id, text string) error {
	return a.svc.Comment(ctx, actor, id, text)
}
func (a issueAdapter) TransitionIssue(ctx context.Context, actor issues.Actor, id string, to domain.IssueStatus, comment string) (domain.Issue, error) {
	return a.svc.Transition(ctx, actor, id, to, comment)
}
func (a issueAdapter) LeaveIssue(ctx context.Context, actor issues.Actor, id string) error {
	return a.svc.Leave(ctx, actor, id)
}
func (a issueAdapter) OpenIssueCount(ctx context.Context, actor issues.Actor) (int, error) {
	return a.svc.OpenCount(ctx, actor)
}

type issueBody struct {
	ID              string           `json:"id"`
	LibraryItemID   string           `json:"libraryItemId"`
	Title           string           `json:"title"`
	MediaType       domain.MediaType `json:"mediaType"`
	Source          string           `json:"source"`
	Kind            domain.IssueKind `json:"kind"`
	KindLabel       string           `json:"kindLabel"`
	Part            domain.Selection `json:"part"`
	Status          string           `json:"status"`
	Description     string           `json:"description,omitempty"`
	Reporters       int              `json:"reporters"`
	GoneFromLibrary bool             `json:"goneFromLibrary"`
	ArtworkURL      string           `json:"artworkUrl,omitempty"`
	Link            string           `json:"link,omitempty"`
	Year            int              `json:"year,omitempty"`
	SourceKind      string           `json:"sourceKind,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
	ResolvedAt      *time.Time       `json:"resolvedAt,omitempty"`
}

func toIssueBody(is domain.Issue) issueBody {
	return issueBody{ID: is.ID, LibraryItemID: is.LibraryItemID, Title: is.Title, MediaType: is.MediaType, Source: is.SourceName, Kind: is.Kind, KindLabel: is.Kind.Label(),
		Part: is.Part, Status: string(is.Status), Description: is.Description, Reporters: is.Reporters, GoneFromLibrary: is.GoneFromLibrary, ArtworkURL: is.ArtworkURL, Link: is.Link, Year: is.Year, SourceKind: is.SourceKind, CreatedAt: is.CreatedAt.UTC(), UpdatedAt: is.UpdatedAt.UTC(), ResolvedAt: is.ResolvedAt}
}

func writeIssueError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, issues.ErrForbidden):
		apierror.Write(w, r, 403, "forbidden", "you cannot do that")
	case errors.Is(err, issues.ErrNotFound):
		apierror.Write(w, r, 404, "issue_not_found", "no such issue or library item")
	case errors.Is(err, issues.ErrInvalid):
		apierror.Write(w, r, 422, "invalid_issue", "the report is not valid for this item")
	case errors.Is(err, issues.ErrTransition):
		apierror.Write(w, r, 409, "invalid_transition", "that status change is not allowed")
	case errors.Is(err, issues.ErrRateLimited):
		w.Header().Set("Retry-After", "3600")
		apierror.Write(w, r, 429, "rate_limited", "you have reported too many issues recently; try again later")
	case errors.Is(err, issues.ErrBadCursor):
		apierror.Write(w, r, 400, "invalid_cursor", "the cursor is not valid")
	default:
		apierror.Write(w, r, 500, "issue_failed", "the issue could not be processed")
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil || requireJSONEOF(decoder) != nil {
		apierror.Write(w, r, 400, "invalid_json", "invalid JSON body")
		return false
	}
	return true
}

func registerIssueRoutes(rt *routeTable, svc IssueService) {
	actor := func(u auth.User) issues.Actor { return issues.Actor{ID: u.ID, Role: u.Role} }
	rt.handle("POST /issues", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		var in struct {
			LibraryItemID string           `json:"libraryItemId"`
			Kind          domain.IssueKind `json:"kind"`
			Description   string           `json:"description"`
			Part          domain.Selection `json:"part"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		res, err := svc.ReportIssue(r.Context(), issues.ReportInput{Actor: actor(user), ItemID: in.LibraryItemID, Kind: in.Kind, Description: in.Description, Part: in.Part})
		if err != nil {
			writeIssueError(w, r, err)
			return
		}
		status := http.StatusCreated
		if res.Joined {
			status = http.StatusOK
		}
		writeJSON(w, status, map[string]any{"issue": toIssueBody(res.Issue), "joined": res.Joined})
	})
	rt.handle("GET /issues", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		q := r.URL.Query()
		query := domain.IssueQuery{Status: domain.IssueStatus(q.Get("status")), Kind: domain.IssueKind(q.Get("kind")), MediaType: domain.MediaType(q.Get("mediaType")), After: q.Get("cursor")}
		switch query.Status {
		case "", domain.IssueOpen, domain.IssueInProgress, domain.IssueResolved, domain.IssueClosed:
		default:
			apierror.Write(w, r, 400, "invalid_query", "unknown status")
			return
		}
		if query.Kind != "" && !query.Kind.Valid() {
			apierror.Write(w, r, 400, "invalid_query", "unknown kind")
			return
		}
		if len(query.After) > 200 {
			apierror.Write(w, r, 400, "invalid_cursor", "the cursor is not valid")
			return
		}
		if raw := q.Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				apierror.Write(w, r, 400, "invalid_query", "limit must be 1 to 100")
				return
			}
			query.Limit = n
		}
		page, err := svc.ListIssues(r.Context(), actor(user), query)
		if err != nil {
			writeIssueError(w, r, err)
			return
		}
		items := make([]issueBody, 0, len(page.Items))
		for _, is := range page.Items {
			items = append(items, toIssueBody(is))
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"items": items, "nextCursor": page.NextCursor})
	})
	rt.handle("GET /issues/summary", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		n, err := svc.OpenIssueCount(r.Context(), actor(user))
		if err != nil {
			writeIssueError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"open": n})
	})
	rt.handle("GET /issues/{id}", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		d, err := svc.IssueDetail(r.Context(), actor(user), r.PathValue("id"))
		if err != nil {
			writeIssueError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		reporters := d.Reporters
		if reporters == nil {
			reporters = []domain.IssueReporter{}
		}
		events := d.Events
		if events == nil {
			events = []domain.IssueEvent{}
		}
		writeJSON(w, 200, map[string]any{"issue": toIssueBody(d.Issue), "events": events, "reporters": reporters, "youReported": d.YouReported})
	})
	rt.handle("POST /issues/{id}/comments", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		var in struct {
			Text string `json:"text"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if err := svc.CommentOnIssue(r.Context(), actor(user), r.PathValue("id"), in.Text); err != nil {
			writeIssueError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	rt.handle("POST /issues/{id}/status", accessAdmin, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		var in struct {
			Status  domain.IssueStatus `json:"status"`
			Comment string             `json:"comment"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		is, err := svc.TransitionIssue(r.Context(), actor(user), r.PathValue("id"), in.Status, in.Comment)
		if err != nil {
			writeIssueError(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"issue": toIssueBody(is)})
	})
	rt.handle("DELETE /issues/{id}/reporter", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		if err := svc.LeaveIssue(r.Context(), actor(user), r.PathValue("id")); err != nil {
			writeIssueError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
