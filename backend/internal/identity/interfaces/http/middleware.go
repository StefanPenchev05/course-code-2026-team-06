package identityhttp

import (
	"context"
	"net/http"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type principalKey struct{}

func CurrentPrincipal(ctx context.Context) (domain.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(domain.Principal)
	return p, ok
}

// RequireSameOrigin protects cookie-authenticated writes, including login.
func (h *Handler) RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origins := r.Header.Values("Origin")
		if len(origins) != 1 || origins[0] != h.origin {
			h.fail(w, r, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) RequireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie, err := r.Cookie(h.cookie)
		if err != nil {
			h.fail(w, r, domain.ErrUnauthenticated)
			return
		}
		p, err := h.service.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

func (h *Handler) require(next http.Handler, allowed func(domain.Principal, *http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := CurrentPrincipal(r.Context())
		if !ok {
			h.fail(w, r, domain.ErrUnauthenticated)
			return
		}
		if !allowed(p, r) {
			h.fail(w, r, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) RequireVerifiedStudent(next http.Handler) http.Handler {
	return h.require(next, func(p domain.Principal, _ *http.Request) bool { return p.VerifiedStudent() })
}

func (h *Handler) RequireCoordinator(next http.Handler) http.Handler {
	return h.require(next, func(p domain.Principal, _ *http.Request) bool { return p.Coordinator() })
}

// The resolver must return the faculty of a server-loaded resource.
func (h *Handler) RequireFacultyAccess(resolve func(*http.Request) (string, error), next http.Handler) http.Handler {
	return h.RequireCoordinator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		faculty, err := resolve(r)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		p, _ := CurrentPrincipal(r.Context())
		if !p.CanAccessFaculty(faculty) {
			h.fail(w, r, domain.ErrNotFound)
			return
		}
		next.ServeHTTP(w, r)
	}))
}
