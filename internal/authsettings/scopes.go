package authsettings

import "strings"

const (
	// DefaultScopes is what a new setup asks for: the groups scope lets the
	// provider put the person's groups in the ID token.
	DefaultScopes = "openid profile email groups"
	// LegacyScopes is what every sign-in asked for before scopes were a
	// setting. A configuration saved then, and an environment configuration,
	// keep requesting exactly this.
	LegacyScopes = "openid profile email"
)

// NormalizeScopes returns the space-separated scope list to store and send:
// duplicates removed, openid always first. ok is false when a scope contains a
// character RFC 6749 does not allow in a scope token.
func NormalizeScopes(raw string) (string, bool) {
	out := []string{"openid"}
	seen := map[string]bool{"openid": true}
	for _, s := range strings.Fields(raw) {
		for _, r := range s {
			if r < 0x21 || r > 0x7e || r == '"' || r == '\\' {
				return "", false
			}
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(strings.Join(out, " ")) > 1024 {
		return "", false
	}
	return strings.Join(out, " "), true
}

// ScopeList is the list to request for these values. An empty value means the
// configuration predates the setting, so it keeps the legacy request.
func ScopeList(v Values) []string {
	if v.Scopes == "" {
		return strings.Fields(LegacyScopes)
	}
	return strings.Fields(v.Scopes)
}

// scopeAdvertiser is implemented by providers that read the scopes listed in
// the discovery document.
type scopeAdvertiser interface {
	AdvertisedScopes() ([]string, bool)
}

// scopeWarnings explains what a passing test does not guarantee.
func scopeWarnings(p any, v Values) []string {
	requested := false
	for _, s := range ScopeList(v) {
		if s == "groups" {
			requested = true
		}
	}
	a, ok := p.(scopeAdvertiser)
	if !requested || !ok {
		return nil
	}
	listed, known := a.AdvertisedScopes()
	if !known {
		return nil
	}
	for _, s := range listed {
		if s == "groups" {
			return nil
		}
	}
	return []string{"The provider does not list the groups scope, so the groups claim may be missing from ID tokens. Sign-in still works; check the provider's client scopes."}
}
