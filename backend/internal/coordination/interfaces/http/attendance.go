package coordinationhttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

type Handler struct {
	service *application.AttendanceService
	logger  *slog.Logger
}

func New(service *application.AttendanceService, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	coordinator := func(fn http.HandlerFunc) http.Handler { return auth.RequireAuthentication(auth.RequireCoordinator(fn)) }
	mux.Handle("POST /api/v1/sessions/{id}/no-show-reports", auth.RequireSameOrigin(auth.RequireAuthentication(auth.RequireVerifiedStudent(http.HandlerFunc(h.submit)))))
	const path = "/api/v1/coordinator/attendance-reports"
	mux.Handle("GET "+path, coordinator(h.list))
	mux.Handle("GET "+path+"/{id}", coordinator(h.get))
	mux.Handle("POST "+path+"/{id}/confirm", auth.RequireSameOrigin(coordinator(h.confirm)))
	mux.Handle("POST "+path+"/{id}/reject", auth.RequireSameOrigin(coordinator(h.reject)))
}

var errMalformed = errors.New("malformed request")
var errContent = errors.New("content type must be application/json")
var errTooLarge = errors.New("request body too large")

func read[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var zero T
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return zero, errContent
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	var item *T
	err = dec.Decode(&item)
	if err == nil && item != nil {
		var extra json.RawMessage
		err = dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return *item, nil
		}
	}
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		return zero, errTooLarge
	}
	return zero, errMalformed
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code := 500
	switch {
	case errors.Is(err, errMalformed):
		code = 400
	case errors.Is(err, errContent):
		code = 415
	case errors.Is(err, errTooLarge):
		code = 413
	case errors.Is(err, domain.ErrForbidden):
		code = 403
	case errors.Is(err, domain.ErrNotFound):
		code = 404
	case errors.Is(err, domain.ErrConflict):
		code = 409
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrWindow):
		code = 422
	}
	message := "internal server error"
	if code != 500 {
		message = err.Error()
	} else {
		h.logger.ErrorContext(r.Context(), "attendance request failed", "route", r.Pattern, "status", code)
	}
	write(w, code, struct {
		Error string `json:"error"`
	}{message})
}

func result(w http.ResponseWriter, status int, report domain.Report) {
	write(w, status, struct {
		Data domain.Report `json:"data"`
	}{report})
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	in, err := read[struct {
		Description string `json:"description"`
	}](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	report, err := h.service.Submit(r.Context(), actor, r.PathValue("id"), in.Description)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	result(w, 201, report)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	report, err := h.service.Get(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	result(w, 200, report)
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) { h.review(w, r, domain.Confirmed) }
func (h *Handler) reject(w http.ResponseWriter, r *http.Request)  { h.review(w, r, domain.Rejected) }

func (h *Handler) review(w http.ResponseWriter, r *http.Request, status domain.Status) {
	in, err := read[struct {
		Note string `json:"resolution_note"`
	}](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	report, err := h.service.Review(r.Context(), actor, r.PathValue("id"), status, in.Note)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	result(w, 200, report)
}

type pagination struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

func listFilters(raw string) (pagination, domain.Status, error) {
	p, status := pagination{Limit: 25}, domain.Pending
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
			status = domain.Status(values[0])
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

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, status, err := listFilters(r.URL.RawQuery)
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
		reports = []domain.Report{}
	}
	write(w, 200, struct {
		Data       []domain.Report `json:"data"`
		Pagination pagination      `json:"pagination"`
	}{reports, p})
}
