Status: ready-for-agent

# Stored integration secrets

Source: grilling session of 2026-10-25, D024 (partly supersedes D017) in `docs/DECISIONS.md`, `GLOSSARY.md` (Stored secret, Secret source, Master key).

## Problem Statement

Every integration credential must be set in the server environment and needs a restart. The setup wizard can record only the names of those variables, so an admin cannot finish configuring Requestarr from the UI. This conflicts with D017 and the "no secret-valued form fields" rule in `docs/FIRST_RUN.md`.

## Solution

Admins can enter post-bootstrap integration secrets in Setup. They are stored encrypted in PostgreSQL under an operator-supplied master key, never read back, and applied on the next restart. Environment values still win. See D024 for the full rule set.

## User Stories

1. As an admin, I want to enter the TMDB token, IGDB client secret, Jellyfin/Plex/RomM credentials, Navidrome password and Apprise key in Setup, so that I need no env edits for them.
2. As an admin, I want a secret I saved never shown again, so that it cannot leak through the UI or API.
3. As an admin, I want to see each secret's source (`environment`, `stored`, `stored (shadowed by environment)`), so that I know what is in effect.
4. As an admin, I want a warning on Review when a stored secret is shadowed, so that nothing surprising goes live if the env var is removed.
5. As an admin, I want a connection check to use the value the next restart would use, so that I can verify a new secret before restarting.
6. As an admin, I want Review to list "saved, pending restart" secrets, so that I know a restart is required.
7. As an admin, I want to replace or clear a secret after Finish, so that rotating a credential needs no restart-and-edit cycle of the whole draft.
8. As an operator, I want secrets encrypted with a key I supply, so that a database backup alone does not expose credentials.
9. As an operator, I want a wrong or missing master key to fail closed per integration, so that the server still boots and nothing is silently treated as unconfigured.
10. As an auditor, I want each set or clear recorded without the value, so that changes are traceable.

## Implementation Decisions

- Env-only: OIDC client secret, session HMAC secret, master key `REQUESTARR_SECRETS_KEY` (32 bytes, validated at startup). Never auto-generated into the DB.
- AES-256-GCM; each row stores a key id. Without a master key the UI refuses secret values; the env path is unchanged.
- Resolution at startup, before `config.Validate`: environment, then decrypted stored value. A stored secret satisfies the URL-plus-secret pairing rules. Library and Apprise URLs stay env-only.
- Unreadable stored secret: boot continues; the integration reports "stored secret unreadable" in status and Review.
- Write-only API: presence flag and source only. Values are never logged, audited or echoed in errors.
- Set and clear go through a dedicated admin endpoint (CSRF and Origin protected, value-free audit event in the same transaction), available after Finish.
- Navidrome username becomes a plain draft field; only the password is a secret.
- Domain rules (source resolution, shadowing) live outside HTTP and DB code. Encryption sits behind a narrow interface in `internal/`.
- New migration only; applied migrations are not edited. `api/openapi.yaml` is updated first.

## Testing Decisions

- Unit: encrypt/decrypt round trip, wrong key, tampered ciphertext, key id mismatch, resolution and shadowing table tests.
- Integration (PostgreSQL): set, replace, clear, audit rows contain no value, secrets absent from every response and log line.
- Setup checks use the resolved value; frontend smoke test covers the secret field states.

## Out of Scope

Hot reload of live clients, moving Source or Apprise URLs out of env, master key rotation tooling, OIDC or session secrets in the UI, an external secrets manager.

## Further Notes

Backup docs must say the master key is backed up separately from the database.
