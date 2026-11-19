package coordinationhttp

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

type IssueHandler struct {
	service *application.IssueService
	logger  *slog.Logger
}

func NewIssues(service *application.IssueService, logger *slog.Logger) *IssueHandler {
	return &IssueHandler{service: service, logger: logger}
}

func (h *IssueHandler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	mux.Handle("POST /api/v1/issues", auth.RequireSameOrigin(auth.RequireAuthentication(
		auth.RequireVerifiedStudent(http.HandlerFunc(h.submit)))))
}

func (h *IssueHandler) submit(w http.ResponseWriter, r *http.Request) {
	in, err := read[domain.IssueInput](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	report, err := h.service.Submit(r.Context(), actor, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, struct {
		Data domain.Issue `json:"data"`
	}{report})
}

func (h *IssueHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, errMalformed):
		code = http.StatusBadRequest
	case errors.Is(err, errContent):
		code = http.StatusUnsupportedMediaType
	case errors.Is(err, errTooLarge):
		code = http.StatusRequestEntityTooLarge
	case errors.Is(err, domain.ErrInvalid):
		code = http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrForbidden):
		code = http.StatusForbidden
	case errors.Is(err, domain.ErrIssueNotFound):
		code = http.StatusNotFound
	case errors.Is(err, domain.ErrIssueConflict):
		code = http.StatusConflict
	}
	message := "internal server error"
	if code != http.StatusInternalServerError {
		message = err.Error()
	} else {
		h.logger.ErrorContext(r.Context(), "issue request failed", "route", r.Pattern, "status", code)
	}
	write(w, code, struct {
		Error string `json:"error"`
	}{message})
}
