package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// ConnectionCredential is one credential a library Source needs. The value is
// never returned: Configured says whether one exists and Source where it is from.
type ConnectionCredential struct {
	Secret     string `json:"secret"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
	Unreadable bool   `json:"unreadable"`
}

// Connection is one library Source as Settings shows it (D028).
type Connection struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// URLFromEnvironment means the URL comes from its environment variable and
	// cannot be changed here.
	URLFromEnvironment bool   `json:"urlFromEnvironment"`
	URLEnv             string `json:"urlEnv"`
	Saved              bool   `json:"saved"`
	// TakesUsername is true for Sources that sign in with a username.
	TakesUsername bool   `json:"takesUsername"`
	Username      string `json:"username"`
	// Active means the Source is scheduled to sync.
	Active bool `json:"active"`
	// Missing names what a half-set Source still needs; empty when complete or unset.
	Missing     []string               `json:"missing"`
	Credentials []ConnectionCredential `json:"credentials"`
}

// ConnectionTest is the outcome of probing a Source with its saved connection.
type ConnectionTest struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

var (
	// ErrSetByEnvironment is returned when the URL comes from the environment.
	ErrSetByEnvironment = errors.New("the address is set by the environment")
	ErrUnknownSource    = errors.New("unknown library source")
	// ErrUnknownCredential is returned when a credential does not belong to the Source.
	ErrUnknownCredential = errors.New("the credential does not belong to this library source")
)

// ConnectionsService manages where library Sources live. Saving applies at once.
// SaveConnection writes the address and the credentials in one transaction and
// applies them once; an empty c.URL leaves the address as it is, and credentials
// not named are kept. Credential values are never returned or logged.
type ConnectionsService interface {
	ListConnections(context.Context) ([]Connection, error)
	SaveConnection(ctx context.Context, actorID string, c sources.Connection, credentials map[secrets.Name]string) (Connection, error)
	RemoveConnection(ctx context.Context, slug string) (Connection, error)
	TestConnection(ctx context.Context, slug string) (ConnectionTest, error)
}

func registerConnectionRoutes(rt *routeTable, service ConnectionsService) {
	admin := func(pattern string, next func(http.ResponseWriter, *http.Request, string)) {
		rt.handle(pattern, accessSuperuser, func(w http.ResponseWriter, r *http.Request, user auth.User) {
			w.Header().Set("Cache-Control", "no-store")
			next(w, r, user.ID)
		})
	}
	fail := func(w http.ResponseWriter, r *http.Request, err error) {
		switch {
		case errors.Is(err, ErrUnknownSource):
			apierror.Write(w, r, 404, "unknown_source", "unknown library source")
		case errors.Is(err, ErrSetByEnvironment):
			apierror.Write(w, r, 409, "set_by_environment", "this address is set by an environment variable; change it there")
		case errors.Is(err, sources.ErrInvalidConnection):
			apierror.Write(w, r, 422, "invalid_connection", "the address was not saved: enter an http(s) address without a username, query or fragment (and a username where the source needs one)")
		case errors.Is(err, secrets.ErrInvalidValue):
			apierror.Write(w, r, 422, "invalid_secret_value", "the credential was not saved, and nothing was changed: it must be non-empty, at most 4096 characters, without surrounding whitespace or control characters")
		case errors.Is(err, ErrUnknownCredential), errors.Is(err, secrets.ErrUnknownSecret):
			apierror.Write(w, r, 422, "invalid_credential", "the credential does not belong to this library source, and nothing was changed")
		case errors.Is(err, secrets.ErrNoMasterKey):
			apierror.Write(w, r, 409, "secrets_disabled", "the credential was not saved, and nothing was changed: REQUESTARR_SECRETS_KEY is not configured")
		default:
			// Storage and provider errors can carry addresses or credentials.
			apierror.Write(w, r, 500, "connection_update_failed", "could not update the library connection; nothing was saved")
		}
	}
	admin("GET /admin/library-connections", func(w http.ResponseWriter, r *http.Request, _ string) {
		list, err := service.ListConnections(r.Context())
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"connections": list})
	})
	admin("PUT /admin/library-connections/{slug}", func(w http.ResponseWriter, r *http.Request, actorID string) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var in struct {
			URL      string `json:"url"`
			Username string `json:"username"`
			// Credentials maps a secret name to its new value; names left out keep what is saved.
			Credentials map[string]string `json:"credentials"`
		}
		if decoder.Decode(&in) != nil || requireJSONEOF(decoder) != nil {
			apierror.Write(w, r, 400, "invalid_connection_input", "provide one JSON object with url and, where needed, username and credentials")
			return
		}
		var credentials map[secrets.Name]string
		for name, value := range in.Credentials {
			if credentials == nil {
				credentials = map[secrets.Name]string{}
			}
			credentials[secrets.Name(name)] = value
		}
		if in.URL == "" && len(credentials) == 0 {
			fail(w, r, sources.ErrInvalidConnection)
			return
		}
		saved, err := service.SaveConnection(r.Context(), actorID, sources.Connection{Slug: r.PathValue("slug"), URL: in.URL, Username: in.Username}, credentials)
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, 200, saved)
	})
	admin("DELETE /admin/library-connections/{slug}", func(w http.ResponseWriter, r *http.Request, _ string) {
		removed, err := service.RemoveConnection(r.Context(), r.PathValue("slug"))
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, 200, removed)
	})
	admin("POST /admin/library-connections/{slug}/test", func(w http.ResponseWriter, r *http.Request, _ string) {
		result, err := service.TestConnection(r.Context(), r.PathValue("slug"))
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, 200, result)
	})
}
