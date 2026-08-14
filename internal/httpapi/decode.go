package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

var (
	errJSONContentType = errors.New("content type must be application/json")
	errJSONEmpty       = errors.New("request body must contain a JSON document")
	errJSONMalformed   = errors.New("request body contains malformed JSON")
	errJSONUnknown     = errors.New("request body contains an unknown field")
	errJSONMultiple    = errors.New("request body must contain only one JSON document")
	errJSONTooLarge    = errors.New("request body exceeds the maximum size")
)

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64, requireContentType bool) error {
	if requireContentType {
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 {
			return errJSONContentType
		}
		mediaType, _, err := mime.ParseMediaType(contentTypes[0])
		if err != nil || mediaType != "application/json" {
			return errJSONContentType
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return classifyJSONError(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errJSONMultiple
		}
		if classified := classifyJSONError(err); errors.Is(classified, errJSONTooLarge) {
			return classified
		}
		return errJSONMultiple
	}
	return nil
}

func classifyJSONError(err error) error {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return errJSONTooLarge
	case errors.Is(err, io.EOF):
		return errJSONEmpty
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return errJSONUnknown
	default:
		return errJSONMalformed
	}
}
