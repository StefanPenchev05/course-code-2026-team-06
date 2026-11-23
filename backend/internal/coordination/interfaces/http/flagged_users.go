package coordinationhttp

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

type FlaggedUsersHandler struct {
	service *application.FlaggedUsersService
	errors  *IssueHandler
}

func NewFlaggedUsers(service *application.FlaggedUsersService, logger *slog.Logger) *FlaggedUsersHandler {
	return &FlaggedUsersHandler{service: service, errors: &IssueHandler{logger: logger}}
}

func (h *FlaggedUsersHandler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	mux.Handle("GET /api/v1/coordinator/flagged-users", auth.RequireAuthentication(auth.RequireCoordinator(http.HandlerFunc(h.list))))
}

func flaggedUsersFilters(raw string) (pagination, error) {
	p := pagination{Limit: 25}
	params, err := url.ParseQuery(raw)
	if err != nil {
		return p, errMalformed
	}
	for k, values := range params {
		if len(values) != 1 || (k != "limit" && k != "offset") {
			return p, errMalformed
		}
		v, err := strconv.Atoi(values[0])
		if err != nil {
			return p, errMalformed
		}
		if k == "limit" {
			p.Limit = v
		} else {
			p.Offset = v
		}
	}
	return p, nil
}

func (h *FlaggedUsersHandler) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, err := flaggedUsersFilters(r.URL.RawQuery)
	if err != nil {
		h.errors.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	flags, err := h.service.List(r.Context(), actor, p.Limit, p.Offset)
	if err != nil {
		h.errors.fail(w, r, err)
		return
	}
	p.HasMore = len(flags) > p.Limit
	if p.HasMore {
		flags = flags[:p.Limit]
	}
	write(w, 200, struct {
		Data       []domain.FlaggedUser `json:"data"`
		Pagination pagination           `json:"pagination"`
	}{flags, p})
}
