package httpapi

import (
	"context"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/checklist"
)

// ChecklistService computes the administrator's Get started list and remembers
// that it was dismissed (D038).
type ChecklistService interface {
	Get(context.Context) (checklist.Result, error)
	Dismiss(context.Context) error
}

func registerChecklistRoutes(rt *routeTable, list ChecklistService) {
	rt.handle("GET /admin/checklist", accessSuperuser, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		result, err := list.Get(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "checklist_failed", "could not load the Get started checklist")
			return
		}
		writeJSON(w, 200, result)
	})
	rt.handle("POST /admin/checklist/dismiss", accessSuperuser, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		if err := list.Dismiss(r.Context()); err != nil {
			apierror.Write(w, r, 500, "checklist_failed", "could not dismiss the Get started checklist")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
