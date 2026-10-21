CREATE TABLE admin_user_audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id UUID NOT NULL REFERENCES users(id),
    target_user_id UUID NOT NULL REFERENCES users(id),
    previous_role TEXT NOT NULL CHECK (previous_role IN ('admin', 'requester')),
    new_role TEXT NOT NULL CHECK (new_role IN ('admin', 'requester')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX admin_user_audit_target_idx ON admin_user_audit_events(target_user_id, created_at DESC);
