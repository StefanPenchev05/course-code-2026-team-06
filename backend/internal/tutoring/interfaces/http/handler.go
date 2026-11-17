package tutoringhttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type Handler struct {
	service *application.Service
	logger  *slog.Logger
}

func New(service *application.Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	authenticated := func(next http.HandlerFunc) http.Handler { return auth.RequireAuthentication(next) }
	student := func(next http.HandlerFunc) http.Handler {
		return auth.RequireAuthentication(auth.RequireVerifiedStudent(next))
	}
	coordinator := func(next http.HandlerFunc) http.Handler {
		return auth.RequireAuthentication(auth.RequireCoordinator(next))
	}

	mux.Handle("POST /api/v1/tutor-applications", auth.RequireSameOrigin(student(h.apply)))
	mux.Handle("GET /api/v1/tutor-applications/me", authenticated(h.myApplications))
	mux.Handle("GET /api/v1/tutor-applications/{id}", authenticated(h.detail))
	mux.Handle("GET /api/v1/coordinator/tutor-applications", coordinator(h.queue))
	mux.Handle("GET /api/v1/coordinator/tutor-applications/{id}", coordinator(h.reviewDetail))
	mux.Handle("POST /api/v1/coordinator/tutor-applications/{id}/approve", auth.RequireSameOrigin(coordinator(h.approve)))
	mux.Handle("POST /api/v1/coordinator/tutor-applications/{id}/reject", auth.RequireSameOrigin(coordinator(h.reject)))
	mux.Handle("GET /api/v1/tutors", student(h.search))
	mux.Handle("GET /api/v1/tutors/me", student(h.myProfile))
	mux.Handle("GET /api/v1/tutors/{id}", student(h.profile))
}

var (
	errMalformed = errors.New("malformed request")
	errContent   = errors.New("content type must be application/json")
	errSize      = errors.New("request body too large")
)

func decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var zero T
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return zero, errContent
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	var input *T
	err = decoder.Decode(&input)
	if err == nil && input != nil {
		var extra json.RawMessage
		err = decoder.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return *input, nil
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return zero, errSize
	}
	return zero, errMalformed
}

func query(r *http.Request, filters ...string) (url.Values, domain.Page, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, domain.Page{}, errMalformed
	}
	p := domain.Page{Limit: 25}
	for key, entries := range values {
		if len(entries) != 1 {
			return nil, p, errMalformed
		}
		if key == "limit" || key == "offset" {
			n, err := strconv.Atoi(entries[0])
			if err != nil {
				return nil, p, errMalformed
			}
			if key == "limit" {
				p.Limit = n
			} else {
				p.Offset = n
			}
			continue
		}
		allowed := false
		for _, name := range filters {
			allowed = allowed || name == key
		}
		if !allowed {
			return nil, p, errMalformed
		}
	}
	return values, p, nil
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func writeData(w http.ResponseWriter, data any) {
	writeJSON(w, 200, struct {
		Data any `json:"data"`
	}{data})
}

type pagination struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

func writePage[T any](w http.ResponseWriter, p domain.Page, rows []T) {
	more := len(rows) > p.Limit
	if more {
		rows = rows[:p.Limit]
	}
	if rows == nil {
		rows = []T{}
	}
	writeJSON(w, 200, struct {
		Data       []T        `json:"data"`
		Pagination pagination `json:"pagination"`
	}{rows, pagination{p.Limit, p.Offset, more}})
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, message := 500, "internal server error"
	switch {
	case errors.Is(err, errMalformed):
		code, message = 400, err.Error()
	case errors.Is(err, errContent):
		code, message = 415, err.Error()
	case errors.Is(err, errSize):
		code, message = 413, err.Error()
	case errors.Is(err, domain.ErrInvalid):
		code, message = 422, err.Error()
	case errors.Is(err, domain.ErrForbidden):
		code, message = 403, domain.ErrForbidden.Error()
	case errors.Is(err, domain.ErrNotFound):
		code, message = 404, domain.ErrNotFound.Error()
	case errors.Is(err, domain.ErrConflict):
		code, message = 409, domain.ErrConflict.Error()
	default:
		h.logger.ErrorContext(r.Context(), "tutoring request failed", "route", r.Pattern, "status", code)
	}
	writeJSON(w, code, struct {
		Error string `json:"error"`
	}{message})
}

func (h *Handler) apply(w http.ResponseWriter, r *http.Request) {
	input, err := decode[struct {
		SubjectID   string          `json:"subject_id"`
		Grade       json.RawMessage `json:"grade"`
		Description string          `json:"description"`
	}](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	grade, err := domain.ParseGrade(string(input.Grade))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	id, err := h.service.Apply(r.Context(), p, application.Submission{SubjectID: input.SubjectID, Grade: grade, Description: input.Description})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 201, struct {
		ID     string        `json:"application_id"`
		Status domain.Status `json:"status"`
	}{id, domain.Pending})
}

func (h *Handler) myApplications(w http.ResponseWriter, r *http.Request) {
	_, page, err := query(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rows, err := h.service.MyApplications(r.Context(), p, page)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, page, rows)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	d, err := h.service.Application(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeData(w, d)
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	values, page, err := query(r, "status")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := domain.Pending
	if values.Has("status") {
		status = domain.Status(values.Get("status"))
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rows, err := h.service.CoordinatorApplications(r.Context(), p, status, page)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, page, rows)
}

func (h *Handler) reviewDetail(w http.ResponseWriter, r *http.Request) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	d, err := h.service.ReviewDetail(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeData(w, d)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	h.review(w, r, domain.Approved, "")
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	input, err := decode[struct {
		Reason string `json:"reason"`
	}](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.review(w, r, domain.Rejected, input.Reason)
}

func (h *Handler) review(w http.ResponseWriter, r *http.Request, status domain.Status, reason string) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	err := h.service.Review(r.Context(), p, r.PathValue("id"), status, reason)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	values, page, err := query(r, "subject_id", "faculty_id", "program_id", "level")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rows, err := h.service.Search(r.Context(), p, domain.Search{
		SubjectID: values.Get("subject_id"), FacultyID: values.Get("faculty_id"),
		ProgramID: values.Get("program_id"), Level: values.Get("level"), Page: page,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, page, rows)
}

func (h *Handler) myProfile(w http.ResponseWriter, r *http.Request) { h.getProfile(w, r, true) }
func (h *Handler) profile(w http.ResponseWriter, r *http.Request)   { h.getProfile(w, r, false) }
func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request, own bool) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	profile, err := h.service.Profile(r.Context(), p, r.PathValue("id"), own)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeData(w, profile)
}
