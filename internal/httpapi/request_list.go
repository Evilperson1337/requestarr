package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

func parseRequestListQuery(raw string) (requests.ListQuery, error) {
	if len(raw) > 4096 {
		return requests.ListQuery{}, requests.ErrInvalidQuery
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return requests.ListQuery{}, requests.ErrInvalidQuery
	}
	for name, entries := range values {
		if len(entries) != 1 {
			return requests.ListQuery{}, requests.ErrInvalidQuery
		}
		switch name {
		case "q", "status", "mediaType", "mine", "sort", "limit", "cursor":
		default:
			return requests.ListQuery{}, requests.ErrInvalidQuery
		}
		if entries[0] == "" && name != "q" {
			if name == "cursor" {
				return requests.ListQuery{}, requests.ErrInvalidCursor
			}
			return requests.ListQuery{}, requests.ErrInvalidQuery
		}
	}
	query := requests.ListQuery{Q: values.Get("q"), Status: domain.RequestStatus(values.Get("status")), MediaType: domain.MediaType(values.Get("mediaType")), Sort: values.Get("sort"), Cursor: values.Get("cursor")}
	if mine, present := values["mine"]; present {
		if mine[0] != "true" && mine[0] != "false" {
			return requests.ListQuery{}, requests.ErrInvalidQuery
		}
		query.Mine = mine[0] == "true"
	}
	if limit, present := values["limit"]; present {
		query.Limit, err = strconv.Atoi(limit[0])
		if err != nil || query.Limit < 1 || query.Limit > 100 {
			return requests.ListQuery{}, requests.ErrInvalidQuery
		}
	}
	return query.Normalize()
}

func writeRequestListError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, requests.ErrInvalidQuery):
		apierror.Write(w, r, http.StatusBadRequest, "invalid_query", "use one supported value per parameter: q (up to 200 characters), status, mediaType, mine (true/false), sort (newest/oldest/status), limit (1–100), cursor")
	case errors.Is(err, requests.ErrInvalidCursor):
		apierror.Write(w, r, http.StatusBadRequest, "invalid_cursor", "invalid cursor; use the returned cursor with the same user, filters, sort, and limit")
	case errors.Is(err, requests.ErrCursorExpired):
		apierror.Write(w, r, http.StatusGone, "cursor_expired", "this page cursor has expired; restart the listing without a cursor")
	case errors.Is(err, requests.ErrQueryTooLarge):
		apierror.Write(w, r, http.StatusUnprocessableEntity, "query_too_large", "more than 10000 requests match; narrow the title, status, media type, or My Requests filter")
	default:
		apierror.Write(w, r, http.StatusInternalServerError, "requests_list_failed", "could not list requests")
	}
}
