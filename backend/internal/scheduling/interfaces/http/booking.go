package schedulinghttp

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type BookingHandler struct {
	logger   *slog.Logger
	bookings *application.BookingService
}

func NewBooking(bookings *application.BookingService, logger *slog.Logger) *BookingHandler {
	return &BookingHandler{logger: logger, bookings: bookings}
}

func (h *BookingHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	schedulingError(h.logger, w, r, err)
}

func (h *BookingHandler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	read := func(fn http.HandlerFunc) http.Handler { return auth.RequireAuthentication(fn) }
	write := func(fn http.HandlerFunc) http.Handler { return auth.RequireSameOrigin(read(fn)) }
	mux.Handle("POST /api/v1/sessions", write(h.request))
	mux.Handle("GET /api/v1/sessions/me", read(h.mySessions))
	mux.Handle("GET /api/v1/sessions/{id}", read(h.session))
	mux.Handle("POST /api/v1/sessions/{id}/cancel", write(h.cancel))
	const tutorRequests = "/api/v1/tutors/me/session-requests"
	mux.Handle("GET "+tutorRequests, read(h.tutorRequests))
	mux.Handle("POST "+tutorRequests+"/{id}/accept", write(h.accept))
	mux.Handle("POST "+tutorRequests+"/{id}/decline", write(h.decline))
}

func (h *BookingHandler) request(w http.ResponseWriter, r *http.Request) {
	input, err := decode[domain.Request](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	session, err := h.bookings.Request(r.Context(), p, input)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 201, session)
}

func (h *BookingHandler) accept(w http.ResponseWriter, r *http.Request) {
	meeting, err := decode[domain.Meeting](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	session, err := h.bookings.Accept(r.Context(), p, r.PathValue("id"), meeting)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 200, session)
}

type reasonInput struct {
	Reason string `json:"reason"`
}

func (h *BookingHandler) decline(w http.ResponseWriter, r *http.Request) {
	input, err := decode[reasonInput](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	session, err := h.bookings.Decline(r.Context(), p, r.PathValue("id"), input.Reason)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 200, session)
}

func (h *BookingHandler) cancel(w http.ResponseWriter, r *http.Request) {
	input, err := decode[reasonInput](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	session, err := h.bookings.Cancel(r.Context(), p, r.PathValue("id"), input.Reason)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 200, session)
}

func (h *BookingHandler) session(w http.ResponseWriter, r *http.Request) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	session, err := h.bookings.Get(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 200, session)
}

func (h *BookingHandler) mySessions(w http.ResponseWriter, r *http.Request) { h.sessions(w, r, false) }
func (h *BookingHandler) tutorRequests(w http.ResponseWriter, r *http.Request) {
	h.sessions(w, r, true)
}

func sessionFilters(r *http.Request, tutorOnly bool) (application.Page, domain.Status, error) {
	p := application.Page{Limit: 25}
	var status domain.Status
	if tutorOnly {
		status = domain.Requested
	}
	params, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return p, status, errMalformed
	}
	for name, values := range params {
		if len(values) != 1 {
			return p, status, errMalformed
		}
		switch name {
		case "status":
			status = domain.Status(values[0])
			if !status.Valid() {
				return p, status, domain.ErrBookingInvalid
			}
		case "limit", "offset":
			n, err := strconv.Atoi(values[0])
			if err != nil {
				return p, status, errMalformed
			}
			if name == "limit" {
				p.Limit = n
			} else {
				p.Offset = n
			}
		default:
			return p, status, errMalformed
		}
	}
	return p, status, nil
}

func (h *BookingHandler) sessions(w http.ResponseWriter, r *http.Request, tutorOnly bool) {
	p, status, err := sessionFilters(r, tutorOnly)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	viewer, _ := identityhttp.CurrentPrincipal(r.Context())
	rows, err := h.bookings.List(r.Context(), viewer, status, tutorOnly, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page(w, p, rows)
}
