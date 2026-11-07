-- An administrator may make a request for another person. The person is the
-- requester (requested_by); this records who acted for them. NULL means the
-- requester made it themselves, which is every request before this change.
ALTER TABLE requests ADD COLUMN made_on_behalf_by UUID REFERENCES users(id) ON DELETE SET NULL;
