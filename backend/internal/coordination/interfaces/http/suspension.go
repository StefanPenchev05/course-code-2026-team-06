package coordinationhttp

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

type SuspensionHandler struct {
	service *application.SuspensionService
	logger  *slog.Logger
}

func NewSuspension(service *application.SuspensionService, logger *slog.Logger) *SuspensionHandler {
	return &SuspensionHandler{service, logger}
}
func (h *SuspensionHandler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	for _, action := range []string{"suspend", "reinstate"} {
		mux.Handle("POST /api/v1/coordinator/users/{id}/"+action, auth.RequireSameOrigin(auth.RequireAuthentication(auth.RequireCoordinator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.change(w, r, action == "suspend")
		})))))
	}
}

func (h *SuspensionHandler) change(w http.ResponseWriter, r *http.Request, suspend bool) {
	if r.URL.RawQuery != "" {
		h.fail(w, r, errMalformed)
		return
	}
	in, err := read[struct {
		Reason string `json:"reason"`
	}](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	result, err := h.service.Change(r.Context(), actor, r.PathValue("id"), in.Reason, suspend)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d := result.Decision
	// Do not return other participants, cross-faculty session IDs, reasons or
	// evidence. The caller already supplied the reason; durable audit retains it.
	write(w, 200, struct {
		Data suspensionView `json:"data"`
	}{suspensionView{
		UserID: d.UserID, DecisionID: d.ID, Suspended: d.IsSuspended, ChangedAt: d.At, CancelledSessions: result.CancelledSessions}})
}

type suspensionView struct {
	UserID            string    `json:"user_id"`
	DecisionID        string    `json:"decision_id"`
	Suspended         bool      `json:"is_suspended"`
	ChangedAt         time.Time `json:"changed_at"`
	CancelledSessions int       `json:"cancelled_sessions"`
}

func (h *SuspensionHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, message := 500, "internal server error"
	switch {
	case errors.Is(err, errMalformed):
		code, message = 400, err.Error()
	case errors.Is(err, errContent):
		code, message = 415, err.Error()
	case errors.Is(err, errTooLarge):
		code, message = 413, err.Error()
	case errors.Is(err, identity.ErrForbidden):
		code, message = 403, "permission denied"
	case errors.Is(err, identity.ErrNotFound):
		code, message = 404, "user not found"
	case errors.Is(err, identity.ErrInvalid):
		code, message = 422, "provide a valid user identifier and a reason of 1–2000 UTF-8 bytes"
	case errors.Is(err, identity.ErrConflict):
		code, message = 409, "account transition conflicts with its current state or a concurrent operation"
	default:
		h.logger.ErrorContext(r.Context(), "account administration failed", "route", r.Pattern, "status", code)
	}
	write(w, code, struct {
		Error string `json:"error"`
	}{message})
}
