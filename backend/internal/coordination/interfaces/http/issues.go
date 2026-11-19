package coordinationhttp

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

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
	coordinator := func(fn http.HandlerFunc) http.Handler { return auth.RequireAuthentication(auth.RequireCoordinator(fn)) }
	const path = "/api/v1/coordinator/issues"
	mux.Handle("GET "+path, coordinator(h.list))
	mux.Handle("GET "+path+"/{id}", coordinator(h.get))
	mux.Handle("POST "+path+"/{id}/resolve", auth.RequireSameOrigin(coordinator(h.resolve)))
}

func (h *IssueHandler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	report, err := h.service.Get(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, struct {
		Data domain.Issue `json:"data"`
	}{report})
}

func (h *IssueHandler) resolve(w http.ResponseWriter, r *http.Request) {
	in, err := read[struct {
		Note string `json:"resolution_note"`
	}](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	report, err := h.service.Resolve(r.Context(), actor, r.PathValue("id"), in.Note)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, struct {
		Data domain.Issue `json:"data"`
	}{report})
}

func issueListFilters(raw string) (pagination, domain.IssueStatus, error) {
	p, status := pagination{Limit: 25}, domain.IssueOpen
	params, err := url.ParseQuery(raw)
	if err != nil {
		return p, status, errMalformed
	}
	for key, values := range params {
		if len(values) != 1 {
			return p, status, errMalformed
		}
		switch key {
		case "status":
			status = domain.IssueStatus(values[0])
		case "limit", "offset":
			v, err := strconv.Atoi(values[0])
			if err != nil {
				return p, status, errMalformed
			}
			if key == "limit" {
				p.Limit = v
			} else {
				p.Offset = v
			}
		default:
			return p, status, errMalformed
		}
	}
	return p, status, nil
}

func (h *IssueHandler) list(w http.ResponseWriter, r *http.Request) {
	p, status, err := issueListFilters(r.URL.RawQuery)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	reports, err := h.service.List(r.Context(), actor, status, p.Limit, p.Offset)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p.HasMore = len(reports) > p.Limit
	if p.HasMore {
		reports = reports[:p.Limit]
	}
	if reports == nil {
		reports = []domain.Issue{}
	}
	write(w, http.StatusOK, struct {
		Data       []domain.Issue `json:"data"`
		Pagination pagination     `json:"pagination"`
	}{reports, p})
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
