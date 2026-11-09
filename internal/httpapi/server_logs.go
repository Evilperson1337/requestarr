package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/logbuffer"
)

// registerServerLogs serves the recent server log records kept in memory
// (D042). It is admin-and-up (D044, amending D042): records can name sources, catalog items and
// provider errors; administrators already see sources and sync runs in Activity.
// The route exists even without a buffer, answering an empty list.
func registerServerLogs(rt *routeTable, buf *logbuffer.Buffer) {
	rt.handle("GET /admin/logs", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		q := r.URL.Query()
		level := slog.LevelDebug
		if raw := q.Get("level"); raw != "" {
			if err := level.UnmarshalText([]byte(raw)); err != nil {
				apierror.Write(w, r, http.StatusBadRequest, "invalid_log_query", "level must be debug, info, warn or error.")
				return
			}
		}
		var after uint64
		if raw := q.Get("after"); raw != "" {
			n, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				apierror.Write(w, r, http.StatusBadRequest, "invalid_log_query", "after must be a record sequence number.")
				return
			}
			after = n
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"entries": buf.Snapshot(level, after), "capacity": logbuffer.Capacity})
	})
}
