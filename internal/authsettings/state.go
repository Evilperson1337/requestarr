// Package authsettings owns runtime OIDC configuration and its unit precedence.
package authsettings

// Values contains plaintext credentials and must never be logged or returned.
type Values struct {
	Issuer, ClientID, ClientSecret string
	// Scopes is empty for the environment and for a configuration saved before
	// scopes were a setting; ScopeList turns that into the legacy request.
	Scopes string
	// GroupsClaim is the ID-token claim the groups are read from; empty means groups.
	GroupsClaim string
}

func (Values) String() string     { return "OIDC values [redacted]" }
func (v Values) GoString() string { return v.String() }

type Resolution struct {
	State     string   `json:"state"`
	Missing   []string `json:"missing"`
	Ignored   []string `json:"ignored"`
	Differing []string `json:"differing"`
	Effective Values   `json:"-"`
}

// Resolve never fills a missing environment value from saved settings.
func Resolve(env Values, saved *Values) Resolution {
	r := Resolution{State: "not_configured", Missing: []string{}, Ignored: []string{}, Differing: []string{}}
	if env == (Values{}) {
		if saved != nil {
			r.State = "saved_only"
			r.Effective = *saved
		}
		return r
	}
	r.Effective = env
	r.State = "environment_only"
	names := []string{"REQUESTARR_OIDC_ISSUER", "REQUESTARR_OIDC_CLIENT_ID", "REQUESTARR_OIDC_CLIENT_SECRET"}
	vals := []string{env.Issuer, env.ClientID, env.ClientSecret}
	for i, v := range vals {
		if v == "" {
			r.Missing = append(r.Missing, names[i])
		}
	}
	if saved != nil {
		r.State = "environment_overriding_saved"
		old := []string{saved.Issuer, saved.ClientID, saved.ClientSecret}
		for i, v := range vals {
			r.Ignored = append(r.Ignored, names[i])
			if old[i] != v {
				r.Differing = append(r.Differing, names[i])
			}
		}
	}
	if len(r.Missing) > 0 {
		r.State = "environment_incomplete"
	}
	return r
}
