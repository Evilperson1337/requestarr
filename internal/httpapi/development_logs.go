package httpapi

import (
	"net/http"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/devlogs"
)

func registerDevelopmentLogs(rt *routeTable, recorder *devlogs.Recorder) {
	if recorder == nil {
		return
	}
	rt.handle("GET /admin/development-logs", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, recorder.Snapshot())
	})
}
