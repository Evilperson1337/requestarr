UPDATE issues SET status='open' WHERE status='in_progress';
DELETE FROM issue_events WHERE event_type='in_progress';
ALTER TABLE issue_events DROP CONSTRAINT issue_events_event_type_check;
ALTER TABLE issue_events ADD CONSTRAINT issue_events_event_type_check CHECK (event_type IN ('reported','reporter_added','commented','resolved','closed','reopened','left'));
DROP INDEX issues_one_open;
CREATE UNIQUE INDEX issues_one_open ON issues (inventory_item_id, kind, part_key) WHERE status = 'open';
ALTER TABLE issues DROP CONSTRAINT issues_status_check;
ALTER TABLE issues ADD CONSTRAINT issues_status_check CHECK (status IN ('open','resolved','closed'));
