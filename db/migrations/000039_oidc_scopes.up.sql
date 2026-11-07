-- The OIDC scopes became a setting. A configuration saved before then asked for
-- openid, profile and email, so keep asking for exactly that: an upgrade must
-- not change the sign-in request. New setups default to adding groups in the
-- application. Nothing is created when no configuration was saved.
UPDATE app_settings
SET value = (value::jsonb || '{"scopes":"openid profile email"}'::jsonb)::text
WHERE key = 'authentication.oidc' AND NOT (value::jsonb ? 'scopes');
