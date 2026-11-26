package cataloghttp

import (
	"errors"
	"net/http"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

func (h *Handler) RegisterSemesters(mux *http.ServeMux, service *application.CalendarService, requireAuth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/semesters", requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := queryParams(r); err != nil {
			h.fail(w, r, err)
			return
		}
		actor, _ := identityhttp.CurrentPrincipal(r.Context())
		rows, err := service.ListForTutor(r.Context(), actor)
		if errors.Is(err, identity.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, struct {
				Error string `json:"error"`
			}{"permission denied"})
			return
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, result[[]domain.Semester]{rows})
	})))
}
