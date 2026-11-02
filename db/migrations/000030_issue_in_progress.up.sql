-- An administrator can mark an Issue as being worked on (D030). It is still an
-- active Issue, so it keeps holding its item/kind/part slot like an open one.
ALTER TABLE issues DROP CONSTRAINT issues_status_check;
ALTER TABLE issues ADD CONSTRAINT issues_status_check CHECK (status IN ('open','in_progress','resolved','closed'));
DROP INDEX issues_one_open;
CREATE UNIQUE INDEX issues_one_open ON issues (inventory_item_id, kind, part_key) WHERE status IN ('open','in_progress');
ALTER TABLE issue_events DROP CONSTRAINT issue_events_event_type_check;
ALTER TABLE issue_events ADD CONSTRAINT issue_events_event_type_check CHECK (event_type IN ('reported','reporter_added','commented','in_progress','resolved','closed','reopened','left'));
