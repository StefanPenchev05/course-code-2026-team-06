package coordinationhttp

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application/monitoring"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type MonitoringHandler struct {
	service *application.MonitoringService
	logger  *slog.Logger
}

func NewMonitoring(service *application.MonitoringService, logger *slog.Logger) *MonitoringHandler {
	return &MonitoringHandler{service, logger}
}
func (h *MonitoringHandler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	coordinator := func(fn http.HandlerFunc) http.Handler { return auth.RequireAuthentication(auth.RequireCoordinator(fn)) }
	mux.Handle("GET /api/v1/coordinator/tutors", coordinator(h.tutors))
	mux.Handle("GET /api/v1/coordinator/tutors/{id}", coordinator(h.tutor))
	mux.Handle("GET /api/v1/coordinator/tutors/{id}/schedule", coordinator(h.schedule))
	mux.Handle("GET /api/v1/coordinator/statistics", coordinator(h.statistics))
}

// Strict parsing also prevents callers from believing a faculty_id override was
// honored. Resource filters are never authority, even when UUID-shaped.
func monitoringQuery(raw, mode string) (url.Values, pagination, error) {
	p := pagination{Limit: 25}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return nil, p, errMalformed
	}
	for key, values := range q {
		if len(values) != 1 || values[0] == "" {
			return nil, p, errMalformed
		}
		switch {
		case mode != "statistics" && (key == "limit" || key == "offset"):
			n, err := strconv.Atoi(values[0])
			if err != nil {
				return nil, p, errMalformed
			}
			if key == "limit" {
				p.Limit = n
			} else {
				p.Offset = n
			}
		case mode == "list" && (key == "program_id" || key == "subject_id"):
		case mode == "schedule" && (key == "from" || key == "to"):
		default:
			return nil, p, errMalformed
		}
	}
	return q, p, nil
}

func monitoringTrim[T any](items []T, p *pagination) []T {
	p.HasMore = len(items) > p.Limit
	if p.HasMore {
		return items[:p.Limit]
	}
	return items
}
func monitoringList[T any](w http.ResponseWriter, items []T, p pagination) {
	items = monitoringTrim(items, &p)
	write(w, http.StatusOK, struct {
		Data       []T        `json:"data"`
		Pagination pagination `json:"pagination"`
	}{items, p})
}
func monitoringData[T any](w http.ResponseWriter, data T) {
	write(w, http.StatusOK, struct {
		Data T `json:"data"`
	}{data})
}

func (h *MonitoringHandler) tutors(w http.ResponseWriter, r *http.Request) {
	q, p, err := monitoringQuery(r.URL.RawQuery, "list")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	items, err := h.service.Tutors(r.Context(), actor, tutoring.MonitoringFilter{
		ProgramID: q.Get("program_id"), SubjectID: q.Get("subject_id"), Page: tutoringdomain.Page{Limit: p.Limit, Offset: p.Offset}})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	monitoringList(w, items, p)
}
func (h *MonitoringHandler) tutor(w http.ResponseWriter, r *http.Request) {
	_, p, err := monitoringQuery(r.URL.RawQuery, "detail")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	item, err := h.service.Tutor(r.Context(), actor, r.PathValue("id"), tutoringdomain.Page{Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	item.Offerings = monitoringTrim(item.Offerings, &p)
	write(w, http.StatusOK, struct {
		Data       application.TutorDetail `json:"data"`
		Pagination pagination              `json:"offerings_pagination"`
	}{item, p})
}
func (h *MonitoringHandler) schedule(w http.ResponseWriter, r *http.Request) {
	q, p, err := monitoringQuery(r.URL.RawQuery, "schedule")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	from, err := time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		h.fail(w, r, application.ErrMonitoringQuery)
		return
	}
	to, err := time.Parse(time.RFC3339, q.Get("to"))
	if err != nil {
		h.fail(w, r, application.ErrMonitoringQuery)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	items, err := h.service.Schedule(r.Context(), actor, r.PathValue("id"), scheduling.Filter{
		From: from, To: to, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	monitoringList(w, items, p)
}
func (h *MonitoringHandler) statistics(w http.ResponseWriter, r *http.Request) {
	if _, _, err := monitoringQuery(r.URL.RawQuery, "statistics"); err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	item, err := h.service.Statistics(r.Context(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	monitoringData(w, item)
}
func (h *MonitoringHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, message := http.StatusInternalServerError, "internal server error"
	switch {
	case errors.Is(err, errMalformed):
		code, message = 400, "invalid query parameters"
	case errors.Is(err, application.ErrMonitoringQuery):
		code, message = 422, err.Error()
	case errors.Is(err, domain.ErrForbidden):
		code, message = 403, "permission denied"
	case errors.Is(err, tutoringdomain.ErrNotFound):
		code, message = 404, "tutor not found"
	default:
		h.logger.ErrorContext(r.Context(), "monitoring request failed", "route", r.Pattern, "status", code)
	}
	write(w, code, struct {
		Error string `json:"error"`
	}{message})
}
