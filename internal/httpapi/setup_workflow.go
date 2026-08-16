package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

const (
	setupWorkflowTimeout       = 2 * time.Second
	setupGeneralBodyMax        = int64(4096)
	setupAuthenticationBodyMax = int64(16 * 1024)
)

type setupStateResponse struct {
	CurrentStep            setup.Step            `json:"current_step"`
	General                *setup.General        `json:"general,omitempty"`
	Authentication         *setup.Authentication `json:"authentication,omitempty"`
	ClientSecretConfigured bool                  `json:"client_secret_configured,omitempty"`
}

type setupAuthenticationRequest struct {
	Issuer                string                   `json:"issuer"`
	ClientID              string                   `json:"client_id"`
	ClientType            setup.ClientType         `json:"client_type"`
	Scopes                []string                 `json:"scopes"`
	GroupsClaim           string                   `json:"groups_claim"`
	DefaultRole           string                   `json:"default_role"`
	GroupRoleMappings     []setup.GroupRoleMapping `json:"group_role_mappings"`
	ClientSecretOperation string                   `json:"client_secret_operation"`
	ClientSecret          string                   `json:"client_secret,omitempty"`
}

type setupGeneralRequest struct {
	InstanceName string `json:"instance_name"`
	PublicURL    string `json:"public_url"`
	Timezone     string `json:"timezone"`
}

func (request *setupGeneralRequest) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("setup General document must be an object")
	}
	seen := make(map[string]bool, 3)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return errors.New("setup General document contains a duplicate field")
		}
		seen[name] = true
		switch name {
		case "instance_name":
			err = decoder.Decode(&request.InstanceName)
		case "public_url":
			err = decoder.Decode(&request.PublicURL)
		case "timezone":
			err = decoder.Decode(&request.Timezone)
		default:
			return errors.New("setup General document contains an unknown field")
		}
		if err != nil {
			return errors.New("setup General fields must be strings")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 3 {
		return errors.New("setup General document must contain exactly the required fields")
	}
	return nil
}

func setupStateHandler(options SetupSessionOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeMethodNotAllowed(w, r, http.MethodGet, http.MethodHead)
			return
		}
		session, ok := setupSessionCookie(r, options.SecureCookie)
		if !ok || options.Workflow == nil {
			writeAPIError(w, r, http.StatusUnauthorized, "setup_session_required", "A valid setup session is required.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), setupWorkflowTimeout)
		defer cancel()
		writeWorkflowResult(w, r, options.Workflow.State(ctx, session))
	})
}

func setupGeneralHandler(options SetupSessionOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeMethodNotAllowed(w, r, http.MethodPut)
			return
		}
		if !exactSetupOrigin(r, options.PublicOrigin) {
			writeAPIError(w, r, http.StatusForbidden, "setup_origin_forbidden", "The request origin is not allowed.")
			return
		}
		csrf := r.Header.Values("X-Requestarr-CSRF")
		if len(csrf) != 1 || csrf[0] != "1" {
			writeAPIError(w, r, http.StatusForbidden, "setup_csrf_forbidden", "The setup CSRF check failed.")
			return
		}
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		var request setupGeneralRequest
		if err := decodeJSON(w, r, &request, setupGeneralBodyMax, false); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		general, err := setup.NormalizeGeneral(setup.General{
			InstanceName: request.InstanceName, PublicURL: request.PublicURL, Timezone: request.Timezone,
		})
		if err != nil {
			writeAPIError(w, r, http.StatusUnprocessableEntity, "setup_general_invalid", "The General setup values are invalid.")
			return
		}
		session, ok := setupSessionCookie(r, options.SecureCookie)
		if !ok || options.Workflow == nil {
			writeAPIError(w, r, http.StatusUnauthorized, "setup_session_required", "A valid setup session is required.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), setupWorkflowTimeout)
		defer cancel()
		writeWorkflowResult(w, r, options.Workflow.PutGeneral(ctx, session, general))
	})
}

func setupAuthenticationHandler(options SetupSessionOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeMethodNotAllowed(w, r, http.MethodPut)
			return
		}
		if !exactSetupOrigin(r, options.PublicOrigin) {
			writeAPIError(w, r, http.StatusForbidden, "setup_origin_forbidden", "The request origin is not allowed.")
			return
		}
		csrf := r.Header.Values("X-Requestarr-CSRF")
		if len(csrf) != 1 || csrf[0] != "1" {
			writeAPIError(w, r, http.StatusForbidden, "setup_csrf_forbidden", "The setup CSRF check failed.")
			return
		}
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		var request setupAuthenticationRequest
		if err := decodeJSON(w, r, &request, setupAuthenticationBodyMax, true); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		authentication, err := setup.NormalizeAuthentication(setup.Authentication{Issuer: request.Issuer, ClientID: request.ClientID, ClientType: request.ClientType,
			Scopes: request.Scopes, GroupsClaim: request.GroupsClaim, DefaultRole: request.DefaultRole, GroupRoles: request.GroupRoleMappings})
		if err != nil {
			writeAPIError(w, r, http.StatusUnprocessableEntity, "setup_authentication_invalid", "The Authentication setup values are invalid.")
			return
		}
		var operation setup.SecretOperation
		switch request.ClientSecretOperation {
		case "preserve":
			if request.ClientSecret != "" {
				writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
				return
			}
			operation = setup.PreserveSecret()
		case "clear":
			if request.ClientSecret != "" {
				writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
				return
			}
			operation = setup.ClearSecret()
		case "replace":
			operation, err = setup.ReplaceSecret(request.ClientSecret)
			if err != nil {
				writeAPIError(w, r, http.StatusUnprocessableEntity, "setup_authentication_invalid", "The Authentication setup values are invalid.")
				return
			}
		default:
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		if (authentication.ClientType == setup.ClientTypePublic && operation.Kind() == setup.SecretPreserve) ||
			(authentication.ClientType == setup.ClientTypePublic && operation.Kind() == setup.SecretReplace) ||
			(authentication.ClientType == setup.ClientTypeConfidential && operation.Kind() == setup.SecretClear) {
			writeAPIError(w, r, http.StatusUnprocessableEntity, "setup_authentication_invalid", "The Authentication setup values are invalid.")
			return
		}
		session, ok := setupSessionCookie(r, options.SecureCookie)
		if !ok || options.Workflow == nil {
			writeAPIError(w, r, http.StatusUnauthorized, "setup_session_required", "A valid setup session is required.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), setupWorkflowTimeout)
		defer cancel()
		writeWorkflowResult(w, r, options.Workflow.PutAuthentication(ctx, session, authentication, operation))
	})
}

func setupSessionCookie(r *http.Request, secure bool) (string, bool) {
	selected := "__Host-requestarr_setup"
	other := "requestarr_setup_dev"
	if !secure {
		selected, other = other, selected
	}
	var value string
	selectedCount := 0
	otherCount := 0
	for _, cookie := range r.Cookies() {
		switch cookie.Name {
		case selected:
			selectedCount++
			value = cookie.Value
		case other:
			otherCount++
		}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	canonical := err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
	return value, selectedCount == 1 && otherCount == 0 && canonical
}

func exactSetupOrigin(r *http.Request, expected string) bool {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	origin, err := setup.NormalizeOrigin(origins[0])
	return err == nil && origin == expected
}

func writeWorkflowResult(w http.ResponseWriter, r *http.Request, result setup.WorkflowResult) {
	switch result.Outcome {
	case setup.WorkflowAvailable:
		writeJSON(w, http.StatusOK, setupStateResponse{CurrentStep: result.State.Current, General: result.State.General,
			Authentication: result.State.Authentication, ClientSecretConfigured: result.State.ClientSecretConfigured})
	case setup.WorkflowSessionRequired:
		writeAPIError(w, r, http.StatusUnauthorized, "setup_session_required", "A valid setup session is required.")
	case setup.WorkflowCompleted:
		writeAPIError(w, r, http.StatusConflict, "setup_completed", "Setup has already been completed.")
	case setup.WorkflowInvalid:
		writeAPIError(w, r, http.StatusUnprocessableEntity, "setup_values_invalid", "The setup values are invalid.")
	case setup.WorkflowLocked:
		writeAPIError(w, r, http.StatusLocked, "setup_locked", "Setup is locked by server configuration.")
	default:
		writeAPIError(w, r, http.StatusServiceUnavailable, "setup_unavailable", "Setup is temporarily unavailable.")
	}
}
