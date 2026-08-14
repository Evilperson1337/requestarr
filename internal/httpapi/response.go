package httpapi

import (
	"encoding/json"
	"net/http"
)

type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, apiErrorEnvelope{Error: apiError{
		Code:      code,
		Message:   message,
		RequestID: requestIDFromContext(r.Context()),
	}})
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request, methods ...string) {
	for _, method := range methods {
		w.Header().Add("Allow", method)
	}
	writeAPIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "The requested method is not allowed for this resource.")
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
