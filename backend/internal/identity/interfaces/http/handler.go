package identityhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type Handler struct {
	service *application.Service
	origin  string
	secure  bool
	cookie  string
	logger  *slog.Logger
}

func New(service *application.Service, origin string, secure bool, logger *slog.Logger) *Handler {
	cookie := "tutti_session"
	if secure {
		cookie = "__Host-tutti_session"
	}
	return &Handler{service: service, origin: origin, secure: secure, cookie: cookie, logger: logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/auth/register", h.RequireSameOrigin(http.HandlerFunc(h.register)))
	mux.Handle("POST /api/v1/auth/login", h.RequireSameOrigin(http.HandlerFunc(h.login)))
	mux.Handle("POST /api/v1/auth/logout", h.RequireSameOrigin(h.RequireAuthentication(http.HandlerFunc(h.logout))))
	mux.Handle("GET /api/v1/auth/me", h.RequireAuthentication(http.HandlerFunc(h.me)))
	mux.Handle("POST /api/v1/auth/resubmit-verification", h.RequireSameOrigin(h.RequireAuthentication(http.HandlerFunc(h.resubmit))))
	mux.Handle("POST /api/v1/auth/activate-coordinator", h.RequireSameOrigin(http.HandlerFunc(h.activate)))
	mux.Handle("GET /api/v1/coordinator/verifications", h.RequireAuthentication(h.RequireCoordinator(http.HandlerFunc(h.list))))
	mux.Handle("GET /api/v1/coordinator/verifications/{id}", h.RequireAuthentication(h.RequireCoordinator(http.HandlerFunc(h.detail))))
	mux.Handle("POST /api/v1/coordinator/verifications/{id}/approve", h.RequireSameOrigin(h.RequireAuthentication(h.RequireCoordinator(http.HandlerFunc(h.approve)))))
	mux.Handle("POST /api/v1/coordinator/verifications/{id}/reject", h.RequireSameOrigin(h.RequireAuthentication(h.RequireCoordinator(http.HandlerFunc(h.reject)))))
}

type studentInput struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	StudentNumber string `json:"student_number"`
	ProgramID     string `json:"program_id"`
}

func (in studentInput) details() domain.StudentDetails {
	return domain.StudentDetails{Name: in.Name, Email: in.Email, StudentNumber: in.StudentNumber, ProgramID: in.ProgramID}
}

type profile struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Email              string        `json:"email"`
	Roles              []string      `json:"roles"`
	VerifiedStudent    bool          `json:"verified_student"`
	Verification       domain.Status `json:"verification_status,omitempty"`
	RejectionReason    string        `json:"rejection_reason,omitempty"`
	CoordinatorFaculty string        `json:"coordinator_faculty_id,omitempty"`
}

func safeProfile(p domain.Principal) profile {
	roles := make([]string, 0, 3)
	if p.Student {
		roles = append(roles, "student")
	}
	if p.CanTutor() {
		roles = append(roles, "tutor")
	}
	if p.Coordinator() {
		roles = append(roles, "coordinator")
	}
	result := profile{ID: p.ID, Name: p.Name, Email: p.Email, Roles: roles, VerifiedStudent: p.VerifiedStudent(),
		Verification: p.Verification, CoordinatorFaculty: p.CoordinatorFaculty}
	if p.Verification == domain.Rejected {
		result.RejectionReason = p.RejectionReason
	}
	return result
}

type verificationCreated struct {
	ID     string        `json:"verification_id"`
	Status domain.Status `json:"verification_status"`
}

var errBadJSON = errors.New("expected a valid JSON object")
var errTooLarge = errors.New("request body too large")
var errContentType = errors.New("content type must be application/json")

func readJSON(w http.ResponseWriter, r *http.Request, target any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return errContentType
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	var object json.RawMessage
	err = dec.Decode(&object)
	if err == nil {
		var extra json.RawMessage
		err = dec.Decode(&extra)
		if errors.Is(err, io.EOF) && len(object) > 0 && object[0] == '{' {
			inner := json.NewDecoder(bytes.NewReader(object))
			inner.DisallowUnknownFields()
			if err := inner.Decode(target); err == nil {
				return nil
			}
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errTooLarge
	}
	return errBadJSON
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	message := "internal server error"
	switch {
	case errors.Is(err, errBadJSON):
		status, message = http.StatusBadRequest, errBadJSON.Error()
	case errors.Is(err, errContentType):
		status, message = http.StatusUnsupportedMediaType, errContentType.Error()
	case errors.Is(err, errTooLarge):
		status, message = http.StatusRequestEntityTooLarge, errTooLarge.Error()
	case errors.Is(err, domain.ErrInvalid):
		status, message = http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, domain.ErrUnauthenticated):
		status, message = http.StatusUnauthorized, domain.ErrUnauthenticated.Error()
	case errors.Is(err, domain.ErrForbidden):
		status, message = http.StatusForbidden, domain.ErrForbidden.Error()
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, domain.ErrNotFound.Error()
	case errors.Is(err, domain.ErrConflict):
		status, message = http.StatusConflict, domain.ErrConflict.Error()
	case errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusServiceUnavailable, "request timed out"
	}
	if status >= 500 {
		h.logger.ErrorContext(r.Context(), "identity request failed", "route", r.Pattern, "status", status)
	}
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{message})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		studentInput
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := h.service.Register(r.Context(), input.details(), input.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, verificationCreated{id, domain.Pending})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		h.fail(w, r, err)
		return
	}
	p, token, err := h.service.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.setCookie(w, token, int(h.service.SessionTTL()/time.Second))
	writeJSON(w, http.StatusOK, safeProfile(p))
}

func (h *Handler) setCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: h.cookie, Value: token, Path: "/", Secure: h.secure, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(h.cookie)
	if err := h.service.Logout(r.Context(), cookie.Value); err != nil {
		h.fail(w, r, err)
		return
	}
	h.setCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	p, _ := CurrentPrincipal(r.Context())
	writeJSON(w, http.StatusOK, safeProfile(p))
}

func (h *Handler) resubmit(w http.ResponseWriter, r *http.Request) {
	var input studentInput
	if err := readJSON(w, r, &input); err != nil {
		h.fail(w, r, err)
		return
	}
	p, _ := CurrentPrincipal(r.Context())
	id, err := h.service.Resubmit(r.Context(), p, input.details())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, verificationCreated{id, domain.Pending})
}

func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.service.Activate(r.Context(), input.Token, input.Password); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := 50, 0
	for key, dst := range map[string]*int{"limit": &limit, "offset": &offset} {
		if value := r.URL.Query().Get(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				h.fail(w, r, domain.ErrInvalid)
				return
			}
			*dst = n
		}
	}
	p, _ := CurrentPrincipal(r.Context())
	result, err := h.service.ListPending(r.Context(), p, limit, offset)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if result == nil {
		result = []domain.Verification{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	p, _ := CurrentPrincipal(r.Context())
	v, err := h.service.Verification(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	h.review(w, r, domain.Approved, "")
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(w, r, &input); err != nil {
		h.fail(w, r, err)
		return
	}
	h.review(w, r, domain.Rejected, input.Reason)
}

func (h *Handler) review(w http.ResponseWriter, r *http.Request, status domain.Status, reason string) {
	p, _ := CurrentPrincipal(r.Context())
	if err := h.service.Review(r.Context(), p, r.PathValue("id"), status, reason); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
