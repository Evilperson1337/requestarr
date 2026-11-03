package checks

import (
	"context"

	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// checkedSecret is the secret a check verifies and the noun used for it in
// messages. Library Sources take both from the Source kind registry.
func checkedSecret(target setup.Target) (secrets.Name, string, bool) {
	switch target {
	case setup.TargetTMDB:
		return secrets.TMDBReadToken, "token", true
	case setup.TargetIGDB:
		return secrets.IGDBClientSecret, "client secret", true
	case setup.TargetNotifications:
		return secrets.AppriseConfigKey, "configuration key", true
	}
	slug, ok := setup.LibrarySource(target)
	if !ok {
		return "", "", false
	}
	kind, _ := sources.Lookup(slug)
	for _, credential := range kind.Credentials {
		if credential.Secret != "" {
			return secrets.Name(credential.Secret), credential.Noun, true
		}
	}
	return "", "", false
}

// Describe is the administrator-facing message for a check result: what the
// code means for this target, and which secret source was verified. It never
// includes a credential value.
func (c *Checker) Describe(ctx context.Context, target setup.Target, code setup.CheckCode) string {
	return describe(target, code) + c.secretNote(ctx, target, code)
}

// describe is the wording alone, without the secret note.
func describe(target setup.Target, code setup.CheckCode) string {
	switch code {
	case setup.CheckOK:
		return "Connection succeeded. Draft settings remain inactive."
	case setup.CheckDisabled:
		return "Provider is disabled in this draft."
	case setup.CheckEnvironment:
		if target == setup.TargetIGDB {
			return "Save an IGDB client secret and client ID. If REQUESTARR_IGDB_CLIENT_ID is set on the server, the saved client ID must match that override. Stored secrets must be readable with the current REQUESTARR_SECRETS_KEY."
		}
		return "Set the listed environment variables and restart Requestarr before checking this connection."
	case setup.CheckAuthentication:
		return "TMDB rejected the credential (HTTP 401 or 403). Use the API Read Access Token from TMDB API settings, not the v3 API key. Paste only the token, without a Bearer prefix, save it, and check again."
	case setup.CheckIncomplete:
		return "Complete this step's required non-secret fields before checking the connection."
	}
	return "Connection check failed. Verify the service address, network access, and server environment, then retry."
}

// secretNote names the source of the secret a check just used, so an admin
// knows which value was verified.
func (c *Checker) secretNote(ctx context.Context, target setup.Target, code setup.CheckCode) string {
	name, noun, ok := checkedSecret(target)
	if c.config.Secrets == nil || !ok {
		return ""
	}
	r, err := c.config.Secrets.Resolve(ctx, name)
	if err != nil {
		return ""
	}
	// An unreadable stored secret is always worth reporting. The other notes
	// claim a value was checked, which is untrue when nothing was attempted.
	if (code == setup.CheckDisabled || code == setup.CheckIncomplete || code == setup.CheckEnvironment) && r.Source != secrets.SourceUnreadable {
		return ""
	}
	switch r.Source {
	case secrets.SourceEnvironment:
		return " Checked the " + noun + " from the environment."
	case secrets.SourceStored:
		return " Checked the stored " + noun + "."
	case secrets.SourceShadowed:
		return " Checked the " + noun + " from the environment; the stored " + noun + " is shadowed by environment and is not used."
	case secrets.SourceUnreadable:
		return " The stored " + noun + " cannot be read with the current REQUESTARR_SECRETS_KEY; enter it again or restore the key."
	}
	return ""
}
