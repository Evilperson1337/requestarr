package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/issues"
)

// issueStore is the Issue repository. It is its own type because Store already
// has a Transition and a Get for other aggregates.
type issueStore struct{ s *Store }

var _ issues.Repository = issueStore{}

// Issues returns the repository for Issues (D030).
func (s *Store) Issues() issues.Repository { return issueStore{s} }

// LibraryItem reads the facts a report needs about one Library item.
func (st issueStore) LibraryItem(ctx context.Context, id string) (issues.LibraryItem, error) {
	if !domain.ValidUUID(id) {
		return issues.LibraryItem{}, issues.ErrNotFound
	}
	var item issues.LibraryItem
	err := st.s.pool.QueryRow(ctx, `SELECT i.id::text,i.title,i.media_type,src.name FROM inventory_items i JOIN library_sources src ON src.id=i.source_id WHERE i.id=$1`, id).Scan(&item.ID, &item.Title, &item.MediaType, &item.SourceName)
	if errors.Is(err, pgx.ErrNoRows) {
		return issues.LibraryItem{}, issues.ErrNotFound
	}
	return item, err
}

const issueColumns = `iss.id::text,iss.inventory_item_id::text,i.title,i.media_type,src.name,iss.kind,iss.part,iss.status,iss.created_by::text,iss.created_at,iss.updated_at,iss.resolved_at,iss.version,
	(SELECT count(*) FROM issue_reporters r WHERE r.issue_id=iss.id AND r.left_at IS NULL),
	COALESCE((SELECT r.note FROM issue_reporters r WHERE r.issue_id=iss.id AND r.user_id=iss.created_by),''),
	i.last_seen_at < COALESCE((SELECT max(observed_at) FROM source_snapshot_runs sr WHERE sr.source_id=i.source_id), i.last_seen_at),
	i.id::text,i.artwork_ref<>'',COALESCE(i.external_url,''),COALESCE(i.year,0),src.kind`

const issueFrom = ` FROM issues iss JOIN inventory_items i ON i.id=iss.inventory_item_id JOIN library_sources src ON src.id=i.source_id`

type rowScanner interface{ Scan(dest ...any) error }

func scanIssue(row rowScanner) (domain.Issue, error) {
	var is domain.Issue
	var part []byte
	var reporters int64
	var itemID, link string
	var hasArt bool
	err := row.Scan(&is.ID, &is.LibraryItemID, &is.Title, &is.MediaType, &is.SourceName, &is.Kind, &part, &is.Status, &is.ReportedBy, &is.CreatedAt, &is.UpdatedAt, &is.ResolvedAt, &is.Version, &reporters, &is.Description, &is.GoneFromLibrary, &itemID, &hasArt, &link, &is.Year, &is.SourceKind)
	if err != nil {
		return domain.Issue{}, err
	}
	is.Reporters = int(reporters)
	if hasArt {
		is.ArtworkURL = "/api/v1/library/items/" + itemID + "/artwork"
	}
	if safeLibraryLink(link) {
		is.Link = link
	}
	if is.Part, err = parseSelection(part); err != nil {
		return domain.Issue{}, err
	}
	return is, nil
}

func (st issueStore) Get(ctx context.Context, id string) (domain.Issue, error) {
	if !domain.ValidUUID(id) {
		return domain.Issue{}, issues.ErrNotFound
	}
	is, err := scanIssue(st.s.pool.QueryRow(ctx, `SELECT `+issueColumns+issueFrom+` WHERE iss.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Issue{}, issues.ErrNotFound
	}
	return is, err
}

// appendIssueEvent records what happened and tells the people it concerns, in the
// same transaction, so an event and its notifications always exist together.
func appendIssueEvent(ctx context.Context, tx pgx.Tx, issueID, eventType, actorID, comment string, now time.Time) error {
	eventID := newUUID()
	if _, err := tx.Exec(ctx, `INSERT INTO issue_events(id,issue_id,event_type,actor_user_id,comment,created_at) VALUES($1,$2,$3,$4,$5,$6)`, eventID, issueID, eventType, actorID, comment, now); err != nil {
		return err
	}
	return notifyIssueEvent(ctx, tx, issueID, eventID, eventType, actorID, comment)
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// notifyIssueEvent: a new Issue tells every admin; an admin's comment and every
// status change tell the Issue's reporters (never the person who acted). The
// global Apprise route hears about new Issues and status changes only, since it
// is one operator channel and not a place for conversation.
func notifyIssueEvent(ctx context.Context, tx pgx.Tx, issueID, eventID, eventType, actorID, comment string) error {
	var title, source string
	var kind domain.IssueKind
	var actorRole string
	if err := tx.QueryRow(ctx, `SELECT i.title,src.name,iss.kind,(SELECT role FROM users WHERE id=$2) FROM issues iss JOIN inventory_items i ON i.id=iss.inventory_item_id JOIN library_sources src ON src.id=i.source_id WHERE iss.id=$1`, issueID, actorID).Scan(&title, &source, &kind, &actorRole); err != nil {
		return err
	}
	key := "issue." + eventType + ":" + eventID
	var heading, body, audience string
	toApprise := false
	switch eventType {
	case "reported":
		heading, audience, toApprise = "New issue: "+title, "admins", true
		body = kind.Label() + " reported on " + title + " (" + source + ")."
		if comment != "" {
			body += " " + shorten(comment, 300)
		}
	case "commented":
		if actorRole != "admin" {
			return nil
		}
		heading, audience = "Update on your issue: "+title, "reporters"
		body = shorten(comment, 400)
	case "in_progress", "resolved", "closed", "reopened":
		verb := map[string]string{"in_progress": "now being worked on", "resolved": "resolved", "closed": "closed without a fix", "reopened": "reopened"}[eventType]
		heading, audience, toApprise = "Issue "+verb+": "+title, "reporters", true
		if eventType == "in_progress" {
			heading = "Issue " + verb + ": " + title
		}
		body = kind.Label() + " on " + title + " (" + source + ") was " + verb + "."
		if comment != "" {
			body += " " + shorten(comment, 300)
		}
	default:
		return nil
	}
	who := `SELECT u.id FROM users u WHERE u.role='admin' AND u.id<>$1`
	if audience == "reporters" {
		who = `SELECT r.user_id FROM issue_reporters r WHERE r.issue_id=$2 AND r.left_at IS NULL AND r.user_id<>$1`
	}
	args := []any{actorID}
	if audience == "reporters" {
		args = append(args, issueID)
	}
	args = append(args, key, shorten(heading, 500), shorten(body, 2000))
	n := len(args)
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO notification_deliveries(id,event_key,user_id,channel,status,title,body,created_at,available_at,updated_at)
		SELECT gen_random_uuid(),$%d,w.id,'in_app','pending',$%d,$%d,now(),now(),now() FROM (%s) w(id)
		ON CONFLICT(event_key,user_id,channel) WHERE event_key IS NOT NULL DO NOTHING`, n-2, n-1, n, who), args...); err != nil {
		return fmt.Errorf("enqueue issue notifications: %w", err)
	}
	if toApprise {
		return enqueueGlobalAppriseEvent(ctx, tx, key, shorten(heading, 500), shorten(body, 2000))
	}
	return nil
}

// ReportOrJoin opens an Issue, or adds the reporter to the open one for the same
// item, kind and part, inside one transaction with the per-user hourly limit.
func (st issueStore) ReportOrJoin(ctx context.Context, in issues.ReportInput, limit int) (domain.Issue, bool, error) {
	tx, err := st.s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, false, err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	// Serialise one user's reports so two quick requests cannot both pass the limit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "issue-report:"+in.Actor.ID); err != nil {
		return domain.Issue{}, false, err
	}
	var recent int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM issue_events WHERE actor_user_id=$1 AND event_type IN ('reported','reporter_added') AND created_at > $2`, in.Actor.ID, now.Add(-time.Hour)).Scan(&recent); err != nil {
		return domain.Issue{}, false, err
	}
	if recent >= limit {
		return domain.Issue{}, false, issues.ErrRateLimited
	}
	partKey, part := in.Part.Key(), []byte("{}")
	if !in.Part.IsAll() {
		if part, err = json.Marshal(in.Part); err != nil {
			return domain.Issue{}, false, err
		}
	}
	var issueID string
	joined := false
	err = tx.QueryRow(ctx, `SELECT id::text FROM issues WHERE inventory_item_id=$1 AND kind=$2 AND part_key=$3 AND status IN ('open','in_progress') FOR UPDATE`, in.ItemID, in.Kind, partKey).Scan(&issueID)
	switch {
	case err == nil:
		joined = true
	case errors.Is(err, pgx.ErrNoRows):
		issueID = newUUID()
		if _, err = tx.Exec(ctx, `INSERT INTO issues(id,inventory_item_id,kind,part,part_key,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, issueID, in.ItemID, in.Kind, part, partKey, in.Actor.ID, now); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return domain.Issue{}, false, fmt.Errorf("a report for this item was just opened; try again: %w", err)
			}
			return domain.Issue{}, false, err
		}
	default:
		return domain.Issue{}, false, err
	}
	if joined {
		ct, err := tx.Exec(ctx, `INSERT INTO issue_reporters(issue_id,user_id,note,created_at) VALUES($1,$2,$3,$4)
			ON CONFLICT (issue_id,user_id) DO UPDATE SET left_at=NULL, note=CASE WHEN EXCLUDED.note<>'' THEN EXCLUDED.note ELSE issue_reporters.note END`, issueID, in.Actor.ID, in.Description, now)
		if err != nil {
			return domain.Issue{}, false, err
		}
		_ = ct
		if err := appendIssueEvent(ctx, tx, issueID, "reporter_added", in.Actor.ID, in.Description, now); err != nil {
			return domain.Issue{}, false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE issues SET updated_at=$2,version=version+1 WHERE id=$1`, issueID, now); err != nil {
			return domain.Issue{}, false, err
		}
	} else {
		if _, err := tx.Exec(ctx, `INSERT INTO issue_reporters(issue_id,user_id,note,created_at) VALUES($1,$2,$3,$4)`, issueID, in.Actor.ID, in.Description, now); err != nil {
			return domain.Issue{}, false, err
		}
		if err := appendIssueEvent(ctx, tx, issueID, "reported", in.Actor.ID, in.Description, now); err != nil {
			return domain.Issue{}, false, err
		}
	}
	is, err := scanIssue(tx.QueryRow(ctx, `SELECT `+issueColumns+issueFrom+` WHERE iss.id=$1`, issueID))
	if err != nil {
		return domain.Issue{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Issue{}, false, err
	}
	return is, joined, nil
}

func (st issueStore) Comment(ctx context.Context, issueID, userID string, admin bool, text string) error {
	if !domain.ValidUUID(issueID) {
		return issues.ErrNotFound
	}
	tx, err := st.s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists, reporter bool
	if err := tx.QueryRow(ctx, `SELECT true,EXISTS(SELECT 1 FROM issue_reporters r WHERE r.issue_id=iss.id AND r.user_id=$2) FROM issues iss WHERE iss.id=$1 FOR UPDATE`, issueID, userID).Scan(&exists, &reporter); errors.Is(err, pgx.ErrNoRows) {
		return issues.ErrNotFound
	} else if err != nil {
		return err
	}
	if !admin && !reporter {
		return issues.ErrForbidden
	}
	now := time.Now().UTC()
	if err := appendIssueEvent(ctx, tx, issueID, "commented", userID, text, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE issues SET updated_at=$2,version=version+1 WHERE id=$1`, issueID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (st issueStore) Transition(ctx context.Context, issueID string, to domain.IssueStatus, actorID, comment string) (domain.Issue, error) {
	if !domain.ValidUUID(issueID) {
		return domain.Issue{}, issues.ErrNotFound
	}
	tx, err := st.s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx)
	var from domain.IssueStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM issues WHERE id=$1 FOR UPDATE`, issueID).Scan(&from); errors.Is(err, pgx.ErrNoRows) {
		return domain.Issue{}, issues.ErrNotFound
	} else if err != nil {
		return domain.Issue{}, err
	}
	if !domain.CanTransitionIssue(from, to) {
		return domain.Issue{}, issues.ErrTransition
	}
	now := time.Now().UTC()
	event := map[domain.IssueStatus]string{domain.IssueResolved: "resolved", domain.IssueClosed: "closed", domain.IssueOpen: "reopened", domain.IssueInProgress: "in_progress"}[to]
	if to.Active() {
		if _, err := tx.Exec(ctx, `UPDATE issues SET status=$3,resolved_by=NULL,resolved_at=NULL,updated_at=$2,version=version+1 WHERE id=$1`, issueID, now, to); err != nil {
			// Another open Issue already holds this item, kind and part.
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return domain.Issue{}, issues.ErrTransition
			}
			return domain.Issue{}, fmt.Errorf("reopen issue: %w", err)
		}
	} else if _, err := tx.Exec(ctx, `UPDATE issues SET status=$2,resolved_by=$3,resolved_at=$4,updated_at=$4,version=version+1 WHERE id=$1`, issueID, to, actorID, now); err != nil {
		return domain.Issue{}, err
	}
	if err := appendIssueEvent(ctx, tx, issueID, event, actorID, comment, now); err != nil {
		return domain.Issue{}, err
	}
	is, err := scanIssue(tx.QueryRow(ctx, `SELECT `+issueColumns+issueFrom+` WHERE iss.id=$1`, issueID))
	if err != nil {
		return domain.Issue{}, err
	}
	return is, tx.Commit(ctx)
}

func (st issueStore) Leave(ctx context.Context, issueID, userID string) error {
	if !domain.ValidUUID(issueID) {
		return issues.ErrNotFound
	}
	tx, err := st.s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `UPDATE issue_reporters SET left_at=now() WHERE issue_id=$1 AND user_id=$2 AND left_at IS NULL`, issueID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 1 {
		if err := appendIssueEvent(ctx, tx, issueID, "left", userID, "", time.Now().UTC()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (st issueStore) List(ctx context.Context, userID string, admin bool, q domain.IssueQuery) (domain.IssuePage, error) {
	args := []any{}
	var where []string
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if !admin {
		add("EXISTS (SELECT 1 FROM issue_reporters r WHERE r.issue_id=iss.id AND r.user_id=$%d)", userID)
	}
	if q.Status != "" {
		add("iss.status=$%d", q.Status)
	}
	if q.Kind != "" {
		add("iss.kind=$%d", q.Kind)
	}
	if q.MediaType != "" {
		add("i.media_type=$%d", q.MediaType)
	}
	if q.After != "" {
		at, id, err := decodeCursor(q.After)
		if err != nil {
			return domain.IssuePage{}, issues.ErrBadCursor
		}
		args = append(args, at, id)
		where = append(where, fmt.Sprintf("(iss.created_at,iss.id) < ($%d,$%d::uuid)", len(args)-1, len(args)))
	}
	limit := q.Limit
	if limit < 1 || limit > 100 {
		limit = 25
	}
	args = append(args, limit+1)
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := st.s.pool.Query(ctx, `SELECT `+issueColumns+issueFrom+cond+fmt.Sprintf(` ORDER BY iss.created_at DESC, iss.id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return domain.IssuePage{}, fmt.Errorf("list issues: %w", err)
	}
	defer rows.Close()
	page := domain.IssuePage{}
	for rows.Next() {
		is, err := scanIssue(rows)
		if err != nil {
			return domain.IssuePage{}, err
		}
		page.Items = append(page.Items, is)
	}
	if err := rows.Err(); err != nil {
		return domain.IssuePage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		cursor := encodeCursor(last.CreatedAt, last.ID)
		page.NextCursor = &cursor
	}
	return page, nil
}

func (st issueStore) Detail(ctx context.Context, issueID, userID string, admin bool) (domain.IssueDetail, error) {
	is, err := st.Get(ctx, issueID)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	var mine bool
	if err := st.s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM issue_reporters WHERE issue_id=$1 AND user_id=$2)`, issueID, userID).Scan(&mine); err != nil {
		return domain.IssueDetail{}, err
	}
	if !admin && !mine {
		return domain.IssueDetail{}, issues.ErrNotFound
	}
	detail := domain.IssueDetail{Issue: is, YouReported: mine}
	events, err := st.s.pool.Query(ctx, `SELECT e.id::text,e.event_type,COALESCE(NULLIF(u.display_name,''),u.email,'Someone'),e.comment,e.created_at FROM issue_events e JOIN users u ON u.id=e.actor_user_id WHERE e.issue_id=$1 ORDER BY e.created_at, e.id`, issueID)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	defer events.Close()
	for events.Next() {
		var ev domain.IssueEvent
		if err := events.Scan(&ev.ID, &ev.Type, &ev.ActorName, &ev.Comment, &ev.At); err != nil {
			return domain.IssueDetail{}, err
		}
		ev.At = ev.At.UTC()
		detail.Events = append(detail.Events, ev)
	}
	if err := events.Err(); err != nil {
		return domain.IssueDetail{}, err
	}
	events.Close()
	filter := ""
	args := []any{issueID}
	if !admin {
		filter = " AND r.user_id=$2"
		args = append(args, userID)
	}
	reporters, err := st.s.pool.Query(ctx, `SELECT r.user_id::text,COALESCE(NULLIF(u.display_name,''),u.email,'Someone'),r.note,r.created_at,r.left_at IS NOT NULL FROM issue_reporters r JOIN users u ON u.id=r.user_id WHERE r.issue_id=$1`+filter+` ORDER BY r.created_at`, args...)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	defer reporters.Close()
	for reporters.Next() {
		var rp domain.IssueReporter
		if err := reporters.Scan(&rp.UserID, &rp.Name, &rp.Note, &rp.At, &rp.Left); err != nil {
			return domain.IssueDetail{}, err
		}
		rp.At = rp.At.UTC()
		detail.Reporters = append(detail.Reporters, rp)
	}
	return detail, reporters.Err()
}

func (st issueStore) OpenCount(ctx context.Context, userID string, admin bool) (int, error) {
	var n int
	q := `SELECT count(*) FROM issues WHERE status IN ('open','in_progress')`
	args := []any{}
	if !admin {
		q += ` AND EXISTS (SELECT 1 FROM issue_reporters r WHERE r.issue_id=issues.id AND r.user_id=$1)`
		args = append(args, userID)
	}
	return n, st.s.pool.QueryRow(ctx, q, args...).Scan(&n)
}
