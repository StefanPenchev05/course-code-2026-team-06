package feedbackhttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

type Handler struct {
	service *application.Service
	logger  *slog.Logger
}

func New(service *application.Service, logger *slog.Logger) *Handler {
	return &Handler{service, logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	mux.Handle("POST /api/v1/sessions/{id}/feedback", auth.RequireSameOrigin(auth.RequireAuthentication(http.HandlerFunc(h.submit))))
	mux.Handle("GET /api/v1/sessions/{id}/feedback", auth.RequireAuthentication(http.HandlerFunc(h.get)))
	mux.Handle("GET /api/v1/tutors/me/feedback", auth.RequireAuthentication(http.HandlerFunc(h.received)))
	mux.Handle("GET /api/v1/tutors/me/activity", auth.RequireAuthentication(http.HandlerFunc(h.activity)))
	mux.Handle("GET /api/v1/coordinator/tutors/{id}/feedback",
		auth.RequireAuthentication(auth.RequireCoordinator(http.HandlerFunc(h.review))))
}

var (
	errBody = errors.New("malformed feedback request")
	errType = errors.New("content type must be application/json")
	errSize = errors.New("request body too large")
)

func input(w http.ResponseWriter, r *http.Request) (domain.Input, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return domain.Input{}, errType
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	var in *domain.Input
	err = dec.Decode(&in)
	if err == nil && in != nil {
		var extra json.RawMessage
		err = dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return *in, nil
		}
	}
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		return domain.Input{}, errSize
	}
	return domain.Input{}, errBody
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func data(w http.ResponseWriter, status int, body any) {
	write(w, status, struct {
		Data any `json:"data"`
	}{body})
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, message := 500, "internal server error"
	switch {
	case errors.Is(err, errBody), errors.Is(err, domain.ErrQuery):
		status, message = 400, err.Error()
	case errors.Is(err, errType):
		status, message = 415, err.Error()
	case errors.Is(err, errSize):
		status, message = 413, err.Error()
	case errors.Is(err, domain.ErrForbidden):
		status, message = 403, err.Error()
	case errors.Is(err, domain.ErrNotFound):
		status, message = 404, err.Error()
	case errors.Is(err, domain.ErrConflict):
		status, message = 409, err.Error()
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrIneligible):
		status, message = 422, err.Error()
	default:
		h.logger.ErrorContext(r.Context(), "feedback request failed", "route", r.Pattern, "status", status)
	}
	write(w, status, struct {
		Error string `json:"error"`
	}{message})
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	in, err := input(w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	entry, err := h.service.Submit(r.Context(), actor, r.PathValue("id"), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 201, entry)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	entry, err := h.service.Get(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 200, entry)
}

func parsePage(r *http.Request) (application.Page, error) {
	p := application.Page{Limit: 25}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return p, domain.ErrQuery
	}
	for key, v := range values {
		if len(v) != 1 || (key != "limit" && key != "offset") {
			return p, domain.ErrQuery
		}
		n, err := strconv.Atoi(v[0])
		if err != nil {
			return p, domain.ErrQuery
		}
		if key == "limit" {
			p.Limit = n
		} else {
			p.Offset = n
		}
	}
	return p, nil
}

type pagination struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

func paginate[T any](items []T, p application.Page) ([]T, pagination) {
	meta := pagination{p.Limit, p.Offset, len(items) > p.Limit}
	if meta.HasMore {
		items = items[:p.Limit]
	}
	if items == nil {
		items = []T{}
	}
	return items, meta
}

func (h *Handler) received(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	items, err := h.service.Received(r.Context(), actor, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items, meta := paginate(items, p)
	write(w, 200, struct {
		Data       []domain.Entry `json:"data"`
		Pagination pagination     `json:"pagination"`
	}{items, meta})
}

func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	review, err := h.service.CoordinatorFeedback(r.Context(), actor, r.PathValue("id"), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items, meta := paginate(review.Entries, p)
	write(w, 200, struct {
		domain.TutorRatingSummary
		Data       []domain.Entry `json:"data"`
		Pagination pagination     `json:"pagination"`
	}{review.TutorRatingSummary, items, meta})
}

type upcoming struct {
	ID          string    `json:"id"`
	SubjectID   string    `json:"subject_id"`
	SubjectName string    `json:"subject_name"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Mode        string    `json:"mode"`
	MeetingLink string    `json:"meeting_link,omitempty"`
	MeetingRoom string    `json:"meeting_room,omitempty"`
}

func (h *Handler) activity(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	activity, err := h.service.Activity(r.Context(), actor, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	sessions, meta := paginate(activity.Upcoming, p)
	items := make([]upcoming, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, upcoming{s.ID, s.SubjectID, s.SubjectName, s.StartsAt, s.EndsAt, string(s.Mode), s.MeetingLink, s.MeetingRoom})
	}
	data(w, 200, struct {
		domain.Statistics
		Upcoming   []upcoming `json:"upcoming_sessions"`
		Pagination pagination `json:"pagination"`
	}{activity.Statistics, items, meta})
}
