package schedulinghttp

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

	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type Handler struct {
	service *application.Service
	logger  *slog.Logger
}

func New(service *application.Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	const own = "/api/v1/tutors/me/availability"
	read := func(fn http.HandlerFunc) http.Handler { return auth.RequireAuthentication(fn) }
	write := func(fn http.HandlerFunc) http.Handler { return auth.RequireSameOrigin(auth.RequireAuthentication(fn)) }
	mux.Handle("GET "+own, read(h.mySlots))
	mux.Handle("GET /api/v1/tutors/{id}/availability", read(h.freeSlots))
	mux.Handle("GET "+own+"/rules", read(h.rules))
	mux.Handle("POST "+own+"/rules", write(h.createRule))
	mux.Handle("PATCH "+own+"/rules/{id}", write(h.updateRule))
	mux.Handle("DELETE "+own+"/rules/{id}", write(h.deleteRule))
	mux.Handle("GET "+own+"/rules/{id}/exceptions", read(h.exceptions))
	mux.Handle("DELETE "+own+"/rules/{id}/exceptions/{date}", write(h.restoreException))
	mux.Handle("POST "+own+"/slots", write(h.createSlot))
	mux.Handle("DELETE "+own+"/slots/{id}", write(h.deleteSlot))
}

var (
	errMalformed = errors.New("malformed request")
	errContent   = errors.New("content type must be application/json")
	errTooLarge  = errors.New("request body too large")
)

func decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var zero T
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return zero, errContent
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	var input *T
	err = dec.Decode(&input)
	if err == nil && input != nil {
		var extra json.RawMessage
		err = dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return *input, nil
		}
	}
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		return zero, errTooLarge
	}
	return zero, errMalformed
}

func respond(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func data(w http.ResponseWriter, code int, value any) {
	respond(w, code, struct {
		Data any `json:"data"`
	}{value})
}

func page[T any](w http.ResponseWriter, p application.Page, rows []T) {
	more := len(rows) > p.Limit
	if more {
		rows = rows[:p.Limit]
	}
	if rows == nil {
		rows = []T{}
	}
	respond(w, 200, struct {
		Data       []T        `json:"data"`
		Pagination pagination `json:"pagination"`
	}{rows, pagination{p.Limit, p.Offset, more}})
}

type pagination struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, message := 500, "internal server error"
	switch {
	case errors.Is(err, errMalformed):
		code, message = 400, err.Error()
	case errors.Is(err, errContent):
		code, message = 415, err.Error()
	case errors.Is(err, errTooLarge):
		code, message = 413, err.Error()
	case errors.Is(err, domain.ErrInvalid):
		code, message = 422, err.Error()
	case errors.Is(err, domain.ErrForbidden):
		code, message = 403, domain.ErrForbidden.Error()
	case errors.Is(err, domain.ErrNotFound):
		code, message = 404, domain.ErrNotFound.Error()
	case errors.Is(err, domain.ErrOverlap), errors.Is(err, domain.ErrReserved), errors.Is(err, domain.ErrConflict):
		code, message = 409, err.Error()
	default:
		h.logger.ErrorContext(r.Context(), "availability request failed", "route", r.Pattern, "status", code)
	}
	respond(w, code, struct {
		Error string `json:"error"`
	}{message})
}

func filters(r *http.Request, dates bool) (application.SlotFilter, error) {
	f := application.SlotFilter{Page: application.Page{Limit: 25}}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return f, errMalformed
	}
	for key, items := range values {
		if len(items) != 1 {
			return f, errMalformed
		}
		switch key {
		case "limit", "offset":
			n, err := strconv.Atoi(items[0])
			if err != nil {
				return f, errMalformed
			}
			if key == "limit" {
				f.Limit = n
			} else {
				f.Offset = n
			}
		case "from", "to":
			if !dates {
				return f, errMalformed
			}
			t, err := time.Parse(time.RFC3339Nano, items[0])
			if err != nil {
				t, err = time.Parse(time.DateOnly, items[0])
			}
			if err != nil || t.IsZero() || t.Nanosecond()%1000 != 0 {
				return f, domain.ErrInvalid
			}
			if key == "from" {
				f.From = t.UTC()
			} else {
				f.To = t.UTC()
			}
		default:
			return f, errMalformed
		}
	}
	return f, nil
}

type ruleInput struct {
	SemesterID string      `json:"semester_id"`
	Weekday    int         `json:"iso_weekday"`
	Start      string      `json:"local_start_time"`
	End        string      `json:"local_end_time"`
	Zone       string      `json:"time_zone"`
	Mode       domain.Mode `json:"allowed_mode"`
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	in, err := decode[ruleInput](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rule, err := h.service.CreateRule(r.Context(), p, domain.Rule{
		SemesterID: in.SemesterID, Weekday: in.Weekday, Start: in.Start, End: in.End, Zone: in.Zone, Mode: in.Mode,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 201, rule)
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
	in, err := decode[application.RulePatch](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rule, err := h.service.UpdateRule(r.Context(), p, r.PathValue("id"), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 200, rule)
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	if err := h.service.DeleteRule(r.Context(), p, r.PathValue("id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) rules(w http.ResponseWriter, r *http.Request) {
	f, err := filters(r, false)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rows, err := h.service.Rules(r.Context(), p, f.Page)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page(w, f.Page, rows)
}

func (h *Handler) exceptions(w http.ResponseWriter, r *http.Request) {
	f, err := filters(r, false)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rows, err := h.service.Exceptions(r.Context(), p, r.PathValue("id"), f.Page)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page(w, f.Page, rows)
}

func (h *Handler) restoreException(w http.ResponseWriter, r *http.Request) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	if err := h.service.RestoreException(r.Context(), p, r.PathValue("id"), r.PathValue("date")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createSlot(w http.ResponseWriter, r *http.Request) {
	in, err := decode[domain.Window](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	slot, err := h.service.CreateSlot(r.Context(), p, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data(w, 201, slot)
}

func (h *Handler) deleteSlot(w http.ResponseWriter, r *http.Request) {
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	if err := h.service.DeleteSlot(r.Context(), p, r.PathValue("id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) mySlots(w http.ResponseWriter, r *http.Request)   { h.slots(w, r, true) }
func (h *Handler) freeSlots(w http.ResponseWriter, r *http.Request) { h.slots(w, r, false) }

func (h *Handler) slots(w http.ResponseWriter, r *http.Request, own bool) {
	f, err := filters(r, true)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := identityhttp.CurrentPrincipal(r.Context())
	rows, err := h.service.Slots(r.Context(), p, r.PathValue("id"), own, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page(w, f.Page, rows)
}
