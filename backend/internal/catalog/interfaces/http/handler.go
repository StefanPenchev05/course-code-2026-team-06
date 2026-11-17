package cataloghttp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
)

type Handler struct {
	service *application.Service
	logger  *slog.Logger
}

func New(service *application.Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/faculties", requireAuth(http.HandlerFunc(h.faculties)))
	mux.Handle("GET /api/v1/faculties/{id}/programs", requireAuth(http.HandlerFunc(h.programs)))
	mux.Handle("GET /api/v1/programs/{id}", requireAuth(http.HandlerFunc(h.program)))
	mux.Handle("GET /api/v1/programs/{id}/subjects", requireAuth(http.HandlerFunc(h.subjects)))
	mux.Handle("GET /api/v1/subjects", requireAuth(http.HandlerFunc(h.subjects)))
	mux.Handle("GET /api/v1/subjects/{id}", requireAuth(http.HandlerFunc(h.subject)))
	mux.HandleFunc("GET /api/v1/public/faculties", h.faculties)
	mux.HandleFunc("GET /api/v1/public/faculties/{id}/programs", h.programs)
}

type result[T any] struct {
	Data T `json:"data"`
}

type pagination struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

var errQuery = errors.New("malformed query parameters")

func queryParams(r *http.Request, allowed ...string) (url.Values, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, errQuery
	}
	for key, list := range values {
		known := false
		for _, name := range allowed {
			known = known || key == name
		}
		if !known || len(list) != 1 {
			return nil, errQuery
		}
	}
	return values, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "internal server error"
	switch {
	case errors.Is(err, errQuery), errors.Is(err, domain.ErrInvalidID):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrFacultyNotFound), errors.Is(err, domain.ErrProgramNotFound), errors.Is(err, domain.ErrSubjectNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, domain.ErrInvalidDegreeLevel), errors.Is(err, domain.ErrInvalidPagination), errors.Is(err, domain.ErrInvalidSearch):
		status, message = http.StatusUnprocessableEntity, err.Error()
	default:
		h.logger.ErrorContext(r.Context(), "catalog request failed", "route", r.Pattern, "status", status)
	}
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{message})
}

func (h *Handler) faculties(w http.ResponseWriter, r *http.Request) {
	if _, err := queryParams(r); err != nil {
		h.fail(w, r, err)
		return
	}
	faculties, err := h.service.ListFaculties(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result[[]domain.Faculty]{faculties})
}

func (h *Handler) programs(w http.ResponseWriter, r *http.Request) {
	params, err := queryParams(r, "level")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	programs, err := h.service.ListPrograms(r.Context(), r.PathValue("id"), domain.DegreeLevel(params.Get("level")))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result[[]domain.Program]{programs})
}

func (h *Handler) program(w http.ResponseWriter, r *http.Request) {
	if _, err := queryParams(r); err != nil {
		h.fail(w, r, err)
		return
	}
	program, err := h.service.GetProgram(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result[domain.ProgramDetails]{program})
}

func (h *Handler) subject(w http.ResponseWriter, r *http.Request) {
	if _, err := queryParams(r); err != nil {
		h.fail(w, r, err)
		return
	}
	subject, err := h.service.GetSubject(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result[domain.Subject]{subject})
}

func subjectFilter(r *http.Request) (application.SubjectFilter, error) {
	params, err := queryParams(r, "q", "limit", "offset")
	if err != nil {
		return application.SubjectFilter{}, err
	}
	f := application.SubjectFilter{Query: params.Get("q"), Limit: application.DefaultLimit}
	for _, param := range []struct {
		key   string
		value *int
	}{{"limit", &f.Limit}, {"offset", &f.Offset}} {
		if params.Has(param.key) {
			n, err := strconv.Atoi(params.Get(param.key))
			if err != nil {
				return application.SubjectFilter{}, errQuery
			}
			*param.value = n
		}
	}
	return f, nil
}

func (h *Handler) subjects(w http.ResponseWriter, r *http.Request) {
	f, err := subjectFilter(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var page application.SubjectPage
	if id := r.PathValue("id"); id != "" {
		page, err = h.service.ListProgramSubjects(r.Context(), id, f)
	} else {
		page, err = h.service.SearchSubjects(r.Context(), f)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Data       []domain.Subject `json:"data"`
		Pagination pagination       `json:"pagination"`
	}{page.Subjects, pagination{f.Limit, f.Offset, page.HasMore}})
}
