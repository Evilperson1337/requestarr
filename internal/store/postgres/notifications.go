package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListInbox(ctx context.Context, userID string) ([]notifications.InboxItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,title,body,link,created_at,read_at FROM notification_deliveries WHERE user_id=$1 AND channel='in_app' ORDER BY created_at DESC,id LIMIT 100`, userID)
	if err != nil {
		return nil, fmt.Errorf("query notification inbox: %w", err)
	}
	defer rows.Close()
	items := make([]notifications.InboxItem, 0)
	for rows.Next() {
		var item notifications.InboxItem
		var readAt *time.Time
		if err := rows.Scan(&item.ID, &item.Title, &item.Body, &item.Link, &item.CreatedAt, &readAt); err != nil {
			return nil, fmt.Errorf("scan notification inbox: %w", err)
		}
		item.ReadAt = readAt
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkInboxRead(ctx context.Context, userID, notificationID string) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE notification_deliveries SET read_at=COALESCE(read_at,now()),updated_at=now() WHERE id=$1 AND user_id=$2 AND channel='in_app'`, notificationID, userID)
	if err != nil {
		return false, fmt.Errorf("mark notification read: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

type leasedDelivery struct {
	ID, UserID, Target, Title, Body string
	Attempts                        int
}

func (s *Store) SetAppriseTarget(ctx context.Context, userID, target string, enabled bool) error {
	if len(target) > 2048 {
		return fmt.Errorf("Apprise target exceeds limit")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO notification_preferences(user_id,apprise_target,enabled,updated_at) VALUES($1,$2,$3,now()) ON CONFLICT(user_id) DO UPDATE SET apprise_target=EXCLUDED.apprise_target,enabled=EXCLUDED.enabled,updated_at=now()`, userID, target, enabled)
	if err != nil {
		return fmt.Errorf("save Apprise preference: %w", err)
	}
	return nil
}

// EnqueueAppriseEvent records exactly one external delivery for a source event.
func (s *Store) EnqueueAppriseEvent(ctx context.Context, tx pgx.Tx, eventKey, title, body string) error {
	return enqueueGlobalAppriseEvent(ctx, tx, eventKey, title, body)
}

func enqueueGlobalAppriseEvent(ctx context.Context, tx pgx.Tx, eventKey, title, body string) error {
	if eventKey == "" || len(eventKey) > 256 || title == "" || len(title) > 512 || len(body) > 2048 {
		return fmt.Errorf("invalid notification event")
	}
	_, err := tx.Exec(ctx, `INSERT INTO notification_outbox(event_key,title,body) VALUES($1,$2,$3) ON CONFLICT(event_key) DO NOTHING`, eventKey, title, body)
	if err != nil {
		return fmt.Errorf("enqueue Apprise event: %w", err)
	}
	return nil
}

// ClaimGlobalApprise atomically leases one global outbox event.
func (s *Store) ClaimGlobalApprise(ctx context.Context, lease time.Duration) (id, title, body, token string, attempts int, err error) {
	if lease < time.Second || lease > 10*time.Minute {
		return "", "", "", "", 0, fmt.Errorf("invalid notification lease duration")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", "", "", 0, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `WITH candidate AS (SELECT id FROM notification_outbox WHERE status='pending' AND attempt_count<8 AND available_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1), claimed AS (UPDATE notification_outbox o SET lease_token=gen_random_uuid(),lease_until=now()+$1::interval,attempt_count=attempt_count+1,updated_at=now() FROM candidate c WHERE o.id=c.id RETURNING o.id::text,o.title,o.body,o.attempt_count,o.lease_token::text) SELECT id,title,body,attempt_count,lease_token FROM claimed`, lease.String()).Scan(&id, &title, &body, &attempts, &token)
	if err == pgx.ErrNoRows {
		if _, e := tx.Exec(ctx, `UPDATE notification_outbox SET status='failed',last_error='maximum delivery attempts reached',lease_until=NULL,lease_token=NULL,updated_at=now() WHERE status='pending' AND attempt_count>=8 AND (lease_until IS NULL OR lease_until<now())`); e != nil {
			return "", "", "", "", 0, e
		}
		if err = tx.Commit(ctx); err != nil {
			return "", "", "", "", 0, err
		}
		return "", "", "", "", 0, nil
	}
	if err != nil {
		return "", "", "", "", 0, fmt.Errorf("claim global Apprise delivery: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", "", "", 0, err
	}
	return id, title, body, token, attempts, nil
}

func (s *Store) CompleteGlobalApprise(ctx context.Context, id, token string, success bool, retryAfter time.Duration) error {
	if token == "" || retryAfter < 0 || retryAfter > 24*time.Hour {
		return fmt.Errorf("invalid notification completion")
	}
	status := "pending"
	if success {
		status = "sent"
	}
	_, err := s.pool.Exec(ctx, `UPDATE notification_outbox SET status=CASE WHEN $3='sent' THEN 'sent' WHEN attempt_count>=8 THEN 'failed' ELSE 'pending' END,sent_at=CASE WHEN $3='sent' THEN now() ELSE sent_at END,available_at=CASE WHEN $3='pending' AND attempt_count<8 THEN now()+$4::interval ELSE available_at END,lease_until=NULL,lease_token=NULL,last_error=CASE WHEN $3='pending' THEN CASE WHEN attempt_count>=8 THEN 'maximum delivery attempts reached' ELSE 'delivery failed' END ELSE NULL END,updated_at=now() WHERE id=$1 AND lease_token=$2 AND status='pending'`, id, token, status, retryAfter.String())
	if err != nil {
		return fmt.Errorf("complete global Apprise delivery: %w", err)
	}
	return nil
}

// ClaimAppriseDelivery atomically leases one delivery. Claims are safe across
// concurrent workers and expired leases become eligible for retry.
func (s *Store) ClaimAppriseDelivery(ctx context.Context, lease time.Duration) (id, userID, target, title, body, token string, attempts int, err error) {
	if lease < time.Second || lease > 10*time.Minute {
		return "", "", "", "", "", "", 0, fmt.Errorf("invalid notification lease duration")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", "", "", "", "", 0, err
	}
	defer tx.Rollback(ctx)
	// Suppress queued rows whose current per-user preference is absent/disabled.
	if _, err = tx.Exec(ctx, `UPDATE notification_deliveries d SET status='suppressed',last_error='Apprise preference disabled or removed',updated_at=now() WHERE d.channel='apprise' AND d.status='pending' AND d.lease_token IS NULL AND NOT EXISTS (SELECT 1 FROM notification_preferences p WHERE p.user_id=d.user_id AND p.enabled AND length(p.apprise_target)>0)`); err != nil {
		return "", "", "", "", "", "", 0, fmt.Errorf("suppress disabled Apprise deliveries: %w", err)
	}
	// Resolve the target from the current preference in the same transaction as
	// the lease. A target edited after this claim applies to subsequent claims.
	err = tx.QueryRow(ctx, `WITH candidate AS (SELECT d.id FROM notification_deliveries d JOIN notification_preferences p ON p.user_id=d.user_id AND p.enabled AND length(p.apprise_target)>0 WHERE d.channel='apprise' AND d.status='pending' AND d.attempt_count<8 AND d.available_at<=now() AND (d.lease_until IS NULL OR d.lease_until<now()) ORDER BY d.available_at,d.created_at FOR UPDATE OF d SKIP LOCKED LIMIT 1), claimed AS (UPDATE notification_deliveries d SET lease_token=gen_random_uuid(),lease_until=now()+$1::interval,attempt_count=attempt_count+1,updated_at=now() FROM candidate c WHERE d.id=c.id RETURNING d.id::text,d.user_id::text,d.title,d.body,d.attempt_count,d.lease_token::text) SELECT c.id,c.user_id,p.apprise_target,c.title,c.body,c.attempt_count,c.lease_token FROM claimed c JOIN notification_preferences p ON p.user_id=c.user_id AND p.enabled AND length(p.apprise_target)>0`, lease.String()).Scan(&id, &userID, &target, &title, &body, &attempts, &token)
	if err != nil {
		if err == pgx.ErrNoRows {
			if _, terminalErr := tx.Exec(ctx, `UPDATE notification_deliveries SET status='failed',last_error='maximum delivery attempts reached',lease_until=NULL,lease_token=NULL,updated_at=now() WHERE channel='apprise' AND status='pending' AND attempt_count>=8 AND (lease_until IS NULL OR lease_until<now())`); terminalErr != nil {
				return "", "", "", "", "", "", 0, fmt.Errorf("fail exhausted Apprise deliveries: %w", terminalErr)
			}
			if err = tx.Commit(ctx); err != nil {
				return "", "", "", "", "", "", 0, err
			}
			return "", "", "", "", "", "", 0, nil
		}
		return "", "", "", "", "", "", 0, fmt.Errorf("claim Apprise delivery: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", "", "", "", "", 0, err
	}
	return id, userID, target, title, body, token, attempts, nil
}

func (s *Store) CompleteAppriseDeliveryWithLease(ctx context.Context, id, token string, success bool, retryAfter time.Duration) error {
	if token == "" || retryAfter < 0 || retryAfter > 24*time.Hour {
		return fmt.Errorf("invalid notification completion")
	}
	status := "pending"
	if success {
		status = "sent"
	}
	_, err := s.pool.Exec(ctx, `UPDATE notification_deliveries SET status=CASE WHEN $3='sent' THEN 'sent' WHEN attempt_count>=8 THEN 'failed' ELSE 'pending' END,sent_at=CASE WHEN $3='sent' THEN now() ELSE sent_at END,available_at=CASE WHEN $3='pending' AND attempt_count<8 THEN now()+$4::interval ELSE available_at END,lease_until=NULL,lease_token=NULL,last_error=CASE WHEN $3='pending' THEN CASE WHEN attempt_count>=8 THEN 'maximum delivery attempts reached' ELSE 'delivery failed' END ELSE NULL END,updated_at=now() WHERE id=$1 AND lease_token=$2 AND channel='apprise' AND status='pending'`, id, token, status, retryAfter.String())
	if err != nil {
		return fmt.Errorf("complete Apprise delivery: %w", err)
	}
	return nil
}

func (s *Store) CompleteAppriseDelivery(ctx context.Context, id string, success bool, retryAfter time.Duration) error {
	if retryAfter < 0 || retryAfter > 24*time.Hour {
		return fmt.Errorf("invalid notification retry delay")
	}
	status := "pending"
	if success {
		status = "sent"
	}
	_, err := s.pool.Exec(ctx, `UPDATE notification_deliveries SET status=$2,sent_at=CASE WHEN $2='sent' THEN now() ELSE sent_at END,available_at=CASE WHEN $2='pending' THEN now()+$3::interval ELSE available_at END,lease_until=NULL,lease_token=NULL,last_error=CASE WHEN $2='pending' THEN 'delivery failed' ELSE NULL END,updated_at=now() WHERE id=$1 AND channel='apprise' AND status='pending' AND lease_token IS NOT NULL`, id, status, retryAfter.String())
	if err != nil {
		return fmt.Errorf("complete Apprise delivery: %w", err)
	}
	return nil
}
