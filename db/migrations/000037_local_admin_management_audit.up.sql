CREATE TABLE local_admin_management_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_user_id UUID NOT NULL REFERENCES users(id),
    target_user_id UUID NOT NULL REFERENCES users(id),
    action TEXT NOT NULL CHECK (action IN ('create', 'disable', 'enable')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
