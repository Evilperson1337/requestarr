-- Value-free audit for OIDC metadata/secret changes committed together.
CREATE TABLE authentication_settings_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_user_id UUID NOT NULL REFERENCES users(id),
    action TEXT NOT NULL CHECK (action IN ('save', 'discard')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
