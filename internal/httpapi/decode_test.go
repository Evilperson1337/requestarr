package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name        string
		body        string
		contentType string
		limit       int64
		requireType bool
		want        payload
		wantErr     error
	}{
		{name: "valid", body: `{"name":"requestarr"}`, contentType: "application/json; charset=utf-8", limit: 64, requireType: true, want: payload{Name: "requestarr"}},
		{name: "optional content type", body: `{"name":"requestarr"}`, limit: 64, want: payload{Name: "requestarr"}},
		{name: "missing required content type", body: `{}`, limit: 64, requireType: true, wantErr: errJSONContentType},
		{name: "wrong content type", body: `{}`, contentType: "text/plain", limit: 64, requireType: true, wantErr: errJSONContentType},
		{name: "empty", contentType: "application/json", limit: 64, requireType: true, wantErr: errJSONEmpty},
		{name: "malformed", body: `{"name":`, contentType: "application/json", limit: 64, requireType: true, wantErr: errJSONMalformed},
		{name: "type mismatch", body: `{"name":7}`, contentType: "application/json", limit: 64, requireType: true, wantErr: errJSONMalformed},
		{name: "unknown field", body: `{"name":"ok","secret":"no"}`, contentType: "application/json", limit: 64, requireType: true, wantErr: errJSONUnknown},
		{name: "multiple documents", body: `{"name":"one"} {"name":"two"}`, contentType: "application/json", limit: 64, requireType: true, wantErr: errJSONMultiple},
		{name: "too large", body: `{"name":"requestarr"}`, contentType: "application/json", limit: 8, requireType: true, wantErr: errJSONTooLarge},
		{name: "too large after first document", body: `{}` + strings.Repeat(" ", 20), contentType: "application/json", limit: 8, requireType: true, wantErr: errJSONTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", tt.contentType)
			response := httptest.NewRecorder()
			var got payload
			err := decodeJSON(response, request, &got, tt.limit, tt.requireType)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("payload = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecodeJSONRejectsRepeatedContentType(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(`{"name":"requestarr"}`))
	request.Header["Content-Type"] = []string{"application/json", "text/plain"}
	response := httptest.NewRecorder()
	var dst struct {
		Name string `json:"name"`
	}

	if err := decodeJSON(response, request, &dst, 64, true); !errors.Is(err, errJSONContentType) {
		t.Fatalf("error = %v, want %v", err, errJSONContentType)
	}
}

func TestNoStore(t *testing.T) {
	handler := noStore(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/future-sensitive-route", nil))

	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestNoStoreAppliesToErrorResponses(t *testing.T) {
	handler := noStore(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]bool{"ok": false})
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/future-sensitive-route", nil))

	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
