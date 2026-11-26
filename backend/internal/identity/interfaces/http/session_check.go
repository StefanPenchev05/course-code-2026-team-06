package identityhttp

import (
	"context"
	"net/http"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

// SessionCheck captures the existing opaque cookie for long-lived transports.
// Each invocation uses the same Redis lookup and current PostgreSQL principal
// checks as RequireAuthentication. The token stays in memory, never in a URL.
func (h *Handler) SessionCheck(r *http.Request) (func(context.Context) (domain.Principal, error), error) {
	var token string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == h.cookie {
			count++
			token = cookie.Value
		}
	}
	if count != 1 {
		return nil, domain.ErrUnauthenticated
	}
	return func(ctx context.Context) (domain.Principal, error) {
		return h.service.Authenticate(ctx, token)
	}, nil
}
