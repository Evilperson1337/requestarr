UPDATE app_settings
SET value = (value::jsonb - 'scopes')::text
WHERE key = 'authentication.oidc';
