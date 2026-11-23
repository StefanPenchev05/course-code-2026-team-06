package coordinationhttp

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type PolicyCancellationHandler struct {
	service *application.PolicyCancellationService
	logger  *slog.Logger
}

func NewPolicyCancellation(service *application.PolicyCancellationService, logger *slog.Logger) *PolicyCancellationHandler {
	return &PolicyCancellationHandler{service, logger}
}
func (h *PolicyCancellationHandler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	mux.Handle("POST /api/v1/coordinator/sessions/{id}/cancel", auth.RequireSameOrigin(auth.RequireAuthentication(auth.RequireCoordinator(http.HandlerFunc(h.cancel)))))
}
func (h *PolicyCancellationHandler) cancel(w http.ResponseWriter, r *http.Request) {
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
	session, err := h.service.Cancel(r.Context(), actor, r.PathValue("id"), in.Reason)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Coordinator action responses expose only workload/cancellation metadata,
	// not the participant-only Session JSON (description, meeting URL, identities).
	write(w, 200, struct {
		Data policyCancellationView `json:"data"`
	}{policyCancellationView{
		ID: session.ID, Status: session.Status, StartsAt: session.StartsAt, EndsAt: session.EndsAt,
		CancelledAt: session.CancelledAt, CancelledBy: session.CancelledBy, Reason: session.CancellationReason,
		Origin: scheduling.CoordinatorPolicyCancellation, Late: session.Late}})
}

type policyCancellationView struct {
	ID          string                        `json:"id"`
	Status      scheduling.Status             `json:"status"`
	StartsAt    time.Time                     `json:"starts_at"`
	EndsAt      time.Time                     `json:"ends_at"`
	CancelledAt *time.Time                    `json:"cancelled_at"`
	CancelledBy string                        `json:"cancelled_by_user_id"`
	Reason      string                        `json:"cancellation_reason"`
	Origin      scheduling.CancellationOrigin `json:"cancellation_origin"`
	Late        bool                          `json:"is_late_cancellation"`
}

func (h *PolicyCancellationHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, message := 500, "internal server error"
	switch {
	case errors.Is(err, errMalformed):
		code, message = 400, err.Error()
	case errors.Is(err, errContent):
		code, message = 415, err.Error()
	case errors.Is(err, errTooLarge):
		code, message = 413, err.Error()
	case errors.Is(err, domain.ErrForbidden):
		code, message = 403, "permission denied"
	case errors.Is(err, scheduling.ErrNotFound):
		code, message = 404, "session not found"
	case errors.Is(err, scheduling.ErrBookingInvalid):
		code, message = 422, "provide a valid session identifier and a reason of 1–2000 UTF-8 bytes"
	case errors.Is(err, scheduling.ErrBookingState), errors.Is(err, scheduling.ErrConflict):
		code, message = 409, "session cannot be cancelled in its current state"
	default:
		h.logger.ErrorContext(r.Context(), "policy cancellation failed", "route", r.Pattern, "status", code)
	}
	write(w, code, struct {
		Error string `json:"error"`
	}{message})
}
